package datasetcapture

import (
	"io"
	"strings"
	"testing"
)

func BenchmarkStreamingChunkDelivery(b *testing.B) {
	chunk := make([]byte, 1024)
	b.Run("client-only", func(b *testing.B) {
		for range b.N {
			for range 64 {
				_, _ = io.Discard.Write(chunk)
			}
		}
	})
	b.Run("client-then-capture", func(b *testing.B) {
		pool := NewBufferPool(64<<10, 512<<20)
		b.ReportAllocs()
		for range b.N {
			buffer := pool.NewBuffer(100 << 20)
			for range 64 {
				_, _ = io.Discard.Write(chunk)
				if err := buffer.TryAppend(chunk); err != nil {
					b.Fatal(err)
				}
			}
			buffer.Release()
		}
	})
}

func BenchmarkInspectLargeMultimodalRequest(b *testing.B) {
	base64Payload := strings.Repeat("A", 10<<20)
	body := []byte(`{"model":"gpt-test","stream":true,"conversation_id":"benchmark-session","messages":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + base64Payload + `"}]}]}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for range b.N {
		metadata, err := InspectRequest("/v1/chat/completions", body)
		if err != nil || metadata.Model != "gpt-test" || !metadata.Stream {
			b.Fatalf("InspectRequest() = %#v, %v", metadata, err)
		}
	}
}
