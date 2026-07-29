package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/HUICHUAN-AI/dto"
)

const (
	defaultNERVSkillsLimit     = 3
	maxNERVSkillsLimit         = 5
	maxNERVSkillBodyRunes      = 1400
	maxNERVSkillsContextRunes  = 6000
	nervBundledSkillsAssetPath = "nerv/5.6-JAILBREAK-NERV/skills"
)

type nervSkill struct {
	Name        string
	Description string
	Body        string
	Keywords    []string
}

type nervSkillScore struct {
	Skill nervSkill
	Score int
}

var nervSkillsCache struct {
	sync.Mutex
	loaded bool
	skills []nervSkill
}

func withNERVSkillsContext(basePrompt string, requestText string, options NERVBridgeOptions) string {
	basePrompt = strings.TrimSpace(basePrompt)
	if !options.SkillsEnabled {
		return basePrompt
	}

	context := buildNERVSkillsContext(requestText, options.SkillsLimit)
	if context == "" {
		return basePrompt
	}
	if basePrompt == "" {
		return context
	}
	return basePrompt + "\n\n" + context
}

func collectNERVChatRequestText(request *dto.GeneralOpenAIRequest) string {
	if request == nil {
		return ""
	}

	parts := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role != "user" {
			continue
		}
		if content := strings.TrimSpace(message.StringContent()); content != "" {
			parts = append(parts, content)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}

	for _, message := range request.Messages {
		if content := strings.TrimSpace(message.StringContent()); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

func collectNERVResponsesRequestText(request *dto.OpenAIResponsesRequest) string {
	if request == nil {
		return ""
	}

	parts := make([]string, 0)
	for _, input := range request.ParseInput() {
		if text := strings.TrimSpace(input.Text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return strings.TrimSpace(string(request.Input))
}

func buildNERVSkillsContext(requestText string, limit int) string {
	selected := selectNERVSkills(requestText, limit)
	if len(selected) == 0 {
		return ""
	}

	blocks := make([]string, 0, len(selected))
	totalRunes := 0
	for _, skill := range selected {
		body := trimRunes(strings.TrimSpace(skill.Body), maxNERVSkillBodyRunes)
		block := strings.TrimSpace("### " + skill.Name + "\n" + skill.Description + "\n\n" + body)
		if block == "" {
			continue
		}
		blockRunes := utf8.RuneCountInString(block)
		if totalRunes+blockRunes > maxNERVSkillsContextRunes {
			remaining := maxNERVSkillsContextRunes - totalRunes
			if remaining <= 0 {
				break
			}
			block = trimRunes(block, remaining)
			blockRunes = utf8.RuneCountInString(block)
		}
		totalRunes += blockRunes
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return ""
	}

	return strings.Join([]string{
		"[NERV 技能模块]",
		"以下内容来自原项目 skills 目录，已按当前用户请求自动选择；请结合这些工作流执行。",
		"",
		strings.Join(blocks, "\n\n"),
	}, "\n")
}

func selectNERVSkills(requestText string, limit int) []nervSkill {
	limit = clampNERVSkillsLimit(limit)
	query := strings.ToLower(strings.TrimSpace(requestText))
	if query == "" {
		return nil
	}

	skills := loadNERVSkills()
	if len(skills) == 0 {
		return nil
	}

	queryTokens := tokenizeNERVText(query)
	scored := make([]nervSkillScore, 0, len(skills))
	for _, skill := range skills {
		score := scoreNERVSkill(skill, query, queryTokens)
		if score <= 0 {
			continue
		}
		scored = append(scored, nervSkillScore{Skill: skill, Score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Skill.Name < scored[j].Skill.Name
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	selected := make([]nervSkill, 0, len(scored))
	for _, item := range scored {
		selected = append(selected, item.Skill)
	}
	return selected
}

func scoreNERVSkill(skill nervSkill, query string, queryTokens map[string]bool) int {
	score := 0
	name := strings.ToLower(skill.Name)
	if strings.Contains(query, name) {
		score += 12
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' }) {
		if part != "" && queryTokens[part] {
			score += 4
		}
	}
	for _, keyword := range skill.Keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		if strings.Contains(query, keyword) {
			score += 5
			continue
		}
		if queryTokens[keyword] {
			score += 2
		}
	}
	return score
}

func loadNERVSkills() []nervSkill {
	nervSkillsCache.Lock()
	defer nervSkillsCache.Unlock()
	if nervSkillsCache.loaded {
		return append([]nervSkill(nil), nervSkillsCache.skills...)
	}
	nervSkillsCache.loaded = true

	skillsDir, exists := findNERVSkillsDir()
	if !exists {
		return nil
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}

	skills := make([]nervSkill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		skill := parseNERVSkillMarkdown(entry.Name(), string(data))
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		if skill.Body == "" && skill.Description == "" {
			continue
		}
		skills = append(skills, skill)
	}
	sort.SliceStable(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	nervSkillsCache.skills = skills
	return append([]nervSkill(nil), skills...)
}

func parseNERVSkillMarkdown(fallbackName string, raw string) nervSkill {
	raw = strings.TrimPrefix(strings.ToValidUTF8(raw, ""), "\ufeff")
	lines := strings.Split(raw, "\n")
	name := fallbackName
	description := ""
	bodyStart := 0

	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if line == "---" {
				bodyStart = i + 1
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "name":
				name = strings.Trim(strings.TrimSpace(value), `"'`)
			case "description":
				description = strings.Trim(strings.TrimSpace(value), `"'`)
			}
		}
	}

	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	return nervSkill{
		Name:        strings.TrimSpace(name),
		Description: description,
		Body:        body,
		Keywords:    buildNERVSkillKeywords(name, description, body),
	}
}

func buildNERVSkillKeywords(name string, description string, body string) []string {
	seen := map[string]bool{}
	keywords := make([]string, 0)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "`'\".。；;，,：:()（）[]【】")
		if value == "" || seen[value] {
			return
		}
		if utf8.RuneCountInString(value) < 2 {
			return
		}
		seen[value] = true
		keywords = append(keywords, value)
	}

	for _, item := range strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' }) {
		add(item)
	}

	if before, after, ok := strings.Cut(description, "Trigger:"); ok {
		description = before + " " + after
		for _, item := range strings.FieldsFunc(after, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；'
		}) {
			add(item)
		}
	}

	for _, token := range tokenizeNERVTextToSlice(description + "\n" + body) {
		add(token)
	}
	return keywords
}

func tokenizeNERVText(text string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range tokenizeNERVTextToSlice(text) {
		tokens[token] = true
	}
	return tokens
}

func tokenizeNERVTextToSlice(text string) []string {
	text = strings.ToLower(text)
	return strings.FieldsFunc(text, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	})
}

func findNERVSkillsDir() (string, bool) {
	candidates := make([]string, 0, 8)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		candidates = append(candidates, filepath.Clean(path))
	}

	if envAssetPath := os.Getenv("NERV_ASSET_PATH"); strings.TrimSpace(envAssetPath) != "" {
		add(filepath.Join(envAssetPath, "skills"))
	}
	if workingDir, err := os.Getwd(); err == nil {
		add(filepath.Join(workingDir, nervBundledSkillsAssetPath))
		add(filepath.Join(workingDir, "build", "HUICHUAN-AI", nervBundledSkillsAssetPath))
		add(filepath.Join(workingDir, ".deploy", "HUICHUAN-AI", nervBundledSkillsAssetPath))
	}
	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		add(filepath.Join(executableDir, nervBundledSkillsAssetPath))
		add(filepath.Join(executableDir, "build", "HUICHUAN-AI", nervBundledSkillsAssetPath))
		add(filepath.Join(filepath.Dir(executableDir), nervBundledSkillsAssetPath))
	}

	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func clampNERVSkillsLimit(limit int) int {
	if limit <= 0 {
		return defaultNERVSkillsLimit
	}
	if limit > maxNERVSkillsLimit {
		return maxNERVSkillsLimit
	}
	return limit
}

func trimRunes(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "\n..."
}

func resetNERVSkillsCacheForTest() {
	nervSkillsCache.Lock()
	defer nervSkillsCache.Unlock()
	nervSkillsCache.loaded = false
	nervSkillsCache.skills = nil
}
