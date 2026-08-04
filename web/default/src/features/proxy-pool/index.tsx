import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { ProxyManagementSection } from '@/features/system-settings/operations/proxy-management-section'

export function ProxyPool() {
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>代理池</span>
          <Badge variant='outline' className='shrink-0'>
            超级管理员
          </Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <ProxyManagementSection />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
