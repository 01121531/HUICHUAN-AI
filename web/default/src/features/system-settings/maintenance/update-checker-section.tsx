/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  CheckCircle2Icon,
  DownloadIcon,
  ExternalLinkIcon,
  GitBranchIcon,
  LoaderCircleIcon,
  RefreshCcwIcon,
  RotateCcwIcon,
  ShieldCheckIcon,
  TriangleAlertIcon,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/ui/markdown'
import { Progress } from '@/components/ui/progress'
import { formatTimestamp, formatTimestampToDate } from '@/lib/format'

import { SettingsSection } from '../components/settings-section'
import {
  getLatestSystemUpdate,
  getSystemUpdateCapability,
  getSystemUpdateStatus,
  startSystemUpdate,
  type SystemUpdateCapability,
  type SystemUpdatePhase,
  type SystemUpdateRelease,
  type SystemUpdateState,
} from './system-update-api'

const UPDATE_REPOSITORY_URL = 'https://github.com/01121531/HUICHUAN-AI'
const ACTIVE_PHASES = new Set<SystemUpdatePhase>([
  'downloading',
  'verifying',
  'staged',
  'restarting',
  'validating',
  'rolling_back',
])
const TERMINAL_PHASES = new Set<SystemUpdatePhase>([
  'succeeded',
  'failed',
  'rolled_back',
])

type UpdateCheckerSectionProps = {
  currentVersion?: string | null
  startTime?: number | null
}

function errorMessage(error: unknown, fallback: string) {
  if (
    typeof error === 'object' &&
    error !== null &&
    'response' in error &&
    typeof error.response === 'object' &&
    error.response !== null &&
    'data' in error.response
  ) {
    const data = error.response.data
    if (
      typeof data === 'object' &&
      data !== null &&
      'message' in data &&
      typeof data.message === 'string'
    ) {
      return data.message
    }
  }
  return error instanceof Error ? error.message : fallback
}

function UpdatePhaseIcon({
  phase,
  size = 'sm',
}: {
  phase?: SystemUpdatePhase
  size?: 'sm' | 'lg'
}) {
  const className = size === 'lg' ? 'h-8 w-8' : 'h-5 w-5'
  if (phase === 'succeeded') {
    return <CheckCircle2Icon className={`${className} text-emerald-500`} />
  }
  if (phase === 'failed' || phase === 'rolled_back') {
    return <TriangleAlertIcon className={`${className} text-amber-500`} />
  }
  return (
    <LoaderCircleIcon
      className={`${className} text-primary shrink-0 animate-spin`}
    />
  )
}

export function UpdateCheckerSection({
  currentVersion,
  startTime,
}: UpdateCheckerSectionProps) {
  const { t } = useTranslation()
  const [checking, setChecking] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [releaseDialogOpen, setReleaseDialogOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [progressOpen, setProgressOpen] = useState(false)
  const [release, setRelease] = useState<SystemUpdateRelease | null>(null)
  const [capability, setCapability] = useState<SystemUpdateCapability | null>(
    null
  )
  const [updateState, setUpdateState] = useState<SystemUpdateState | null>(null)
  const [reconnecting, setReconnecting] = useState(false)
  const terminalToastRef = useRef<SystemUpdatePhase | null>(null)

  const uptime = startTime ? formatTimestamp(startTime) : t('Unknown')
  const version = currentVersion || t('Unknown')
  const active = updateState ? ACTIVE_PHASES.has(updateState.phase) : false

  const reasonLabel = useCallback(
    (reason?: string) => {
      const labels: Record<string, string> = {
        already_latest: t('The current version is already the latest.'),
        container_managed_externally: t(
          'Container deployments must be upgraded by Docker or Kubernetes.'
        ),
        development_build: t('Development builds cannot be upgraded online.'),
        disabled_by_environment: t(
          'Online updates are disabled by the server environment.'
        ),
        executable_not_replaceable: t(
          'The current executable cannot be replaced.'
        ),
        executable_path_unavailable: t(
          'The current executable path is unavailable.'
        ),
        instance_detection_failed: t(
          'The active server instance topology could not be verified.'
        ),
        matching_asset_missing: t(
          'This release does not contain a matching update package for the current platform.'
        ),
        multi_instance_deployment: t(
          'Multi-instance deployments must be upgraded by an external rolling update system.'
        ),
        not_master_node: t('Online updates can only run on the master node.'),
        release_asset_size_invalid: t('The release asset size is invalid.'),
        release_not_stable: t(
          'Prerelease versions cannot be installed online.'
        ),
        release_tag_not_semver: t(
          'This release uses an invalid version tag. Publish a vMAJOR.MINOR.PATCH release to enable online installation.'
        ),
        unsupported_platform: t(
          'Online updates currently support Windows, Linux, and macOS standalone deployments only.'
        ),
        version_overridden_by_environment: t(
          'Remove the VERSION environment override before using online updates.'
        ),
      }
      return reason ? labels[reason] || reason : ''
    },
    [t]
  )

  const phaseLabel = useCallback(
    (phase?: SystemUpdatePhase) => {
      const labels: Record<SystemUpdatePhase, string> = {
        idle: t('Ready to check for updates'),
        downloading: t('Downloading update package...'),
        verifying: t('Verifying SHA-256 checksum...'),
        staged: t('Update verified and ready to restart...'),
        restarting: t('Restarting the service...'),
        validating: t('Waiting for the new version to become healthy...'),
        succeeded: t('Online update completed successfully.'),
        failed: t('Online update failed.'),
        rolling_back: t('New version is unhealthy. Rolling back...'),
        rolled_back: t('The previous version has been restored.'),
      }
      return labels[phase || 'idle']
    },
    [t]
  )

  const refreshStatus = useCallback(async () => {
    try {
      const state = await getSystemUpdateStatus()
      setUpdateState(state)
      setReconnecting(false)
      if (ACTIVE_PHASES.has(state.phase)) setProgressOpen(true)
      if (TERMINAL_PHASES.has(state.phase)) {
        setProgressOpen(true)
        if (terminalToastRef.current !== state.phase) {
          terminalToastRef.current = state.phase
          if (state.phase === 'succeeded') {
            toast.success(t('Online update completed successfully.'))
          } else if (state.phase === 'rolled_back') {
            toast.warning(t('The previous version has been restored.'))
          } else {
            toast.error(t('Online update failed.'))
          }
        }
      }
      return state
    } catch {
      if (updateState && ACTIVE_PHASES.has(updateState.phase)) {
        setReconnecting(true)
      }
      return null
    }
  }, [t, updateState])

  useEffect(() => {
    let cancelled = false
    Promise.all([getSystemUpdateCapability(), getSystemUpdateStatus()])
      .then(([nextCapability, state]) => {
        if (cancelled) return
        setCapability(nextCapability)
        setUpdateState(state)
        if (ACTIVE_PHASES.has(state.phase)) setProgressOpen(true)
      })
      .catch(() => {
        if (!cancelled) setCapability(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!active && !reconnecting) return
    const timer = window.setInterval(refreshStatus, 1500)
    return () => window.clearInterval(timer)
  }, [active, reconnecting, refreshStatus])

  const capabilityText = useMemo(() => {
    if (!capability) return t('Checking online update capability...')
    if (capability.supported) {
      return t('Online update is available for {{platform}}/{{arch}}.', {
        platform: capability.platform,
        arch: capability.arch,
      })
    }
    return reasonLabel(capability.reason)
  }, [capability, reasonLabel, t])

  const handleCheckUpdates = async () => {
    setChecking(true)
    try {
      const data = await getLatestSystemUpdate()
      setRelease(data)
      if (!data.update_available) {
        toast.success(
          t('You are running the latest version ({{version}}).', {
            version: data.current_version,
          })
        )
        return
      }
      setReleaseDialogOpen(true)
    } catch (error) {
      toast.error(errorMessage(error, t('Failed to check for updates')))
    } finally {
      setChecking(false)
    }
  }

  const handleInstall = async () => {
    if (!release) return
    setInstalling(true)
    try {
      const state = await startSystemUpdate(release.id)
      setUpdateState(state)
      terminalToastRef.current = null
      setConfirmOpen(false)
      setReleaseDialogOpen(false)
      setProgressOpen(true)
      toast.success(t('The update task has started.'))
    } catch (error) {
      toast.error(errorMessage(error, t('Failed to start online update')))
    } finally {
      setInstalling(false)
    }
  }

  const goToRelease = () => {
    if (release?.html_url) {
      window.open(release.html_url, '_blank', 'noopener,noreferrer')
    }
  }

  const goToRepository = () => {
    window.open(UPDATE_REPOSITORY_URL, '_blank', 'noopener,noreferrer')
  }

  return (
    <>
      <SettingsSection title={t('System maintenance')}>
        <div className='space-y-6'>
          <div className='grid gap-4 md:grid-cols-2'>
            <div className='rounded-lg border p-4'>
              <div className='text-muted-foreground text-sm'>
                {t('Current version')}
              </div>
              <div className='text-lg font-semibold'>{version}</div>
            </div>
            <div className='rounded-lg border p-4'>
              <div className='text-muted-foreground text-sm'>
                {t('Uptime since')}
              </div>
              <div className='text-lg font-semibold'>{uptime}</div>
            </div>
          </div>

          <div className='bg-muted/35 flex items-start gap-3 rounded-lg border p-4'>
            <ShieldCheckIcon className='text-primary mt-0.5 h-5 w-5 shrink-0' />
            <div className='min-w-0 space-y-1'>
              <div className='text-sm font-medium'>{t('Online update')}</div>
              <p className='text-muted-foreground text-sm'>{capabilityText}</p>
            </div>
          </div>

          {updateState && updateState.phase !== 'idle' ? (
            <button
              type='button'
              className='hover:bg-muted/50 flex w-full items-center gap-3 rounded-lg border p-4 text-start transition-colors'
              onClick={() => setProgressOpen(true)}
            >
              <UpdatePhaseIcon phase={updateState.phase} />
              <div className='min-w-0 flex-1'>
                <div className='text-sm font-medium'>
                  {phaseLabel(updateState.phase)}
                </div>
                <div className='text-muted-foreground text-xs'>
                  {updateState.current_version} → {updateState.target_version}
                </div>
              </div>
              <Badge variant='secondary'>{updateState.progress}%</Badge>
            </button>
          ) : null}

          <div className='flex flex-wrap items-center gap-2'>
            <Button onClick={handleCheckUpdates} disabled={checking || active}>
              {checking ? (
                t('Checking updates...')
              ) : (
                <>
                  <RefreshCcwIcon className='me-2 h-4 w-4' />
                  {t('Check for updates')}
                </>
              )}
            </Button>
            <Button type='button' variant='outline' onClick={goToRepository}>
              <GitBranchIcon className='me-2 h-4 w-4' />
              {t('Open repository')}
            </Button>
          </div>
        </div>
      </SettingsSection>

      <Dialog
        open={releaseDialogOpen}
        onOpenChange={setReleaseDialogOpen}
        title={
          release?.tag_name
            ? t('New version available: {{version}}', {
                version: release.tag_name,
              })
            : t('Release details')
        }
        description={
          release?.published_at
            ? `${t('Published')} ${formatTimestampToDate(
                new Date(release.published_at).getTime(),
                'milliseconds'
              )}`
            : undefined
        }
        contentClassName='max-h-[80vh] overflow-y-auto'
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              type='button'
              variant='secondary'
              onClick={() => setReleaseDialogOpen(false)}
            >
              {t('Close')}
            </Button>
            {release?.html_url ? (
              <Button type='button' variant='outline' onClick={goToRelease}>
                <ExternalLinkIcon className='me-2 h-4 w-4' />
                {t('Open release')}
              </Button>
            ) : null}
            {release?.installable ? (
              <Button type='button' onClick={() => setConfirmOpen(true)}>
                <DownloadIcon className='me-2 h-4 w-4' />
                {t('Install online')}
              </Button>
            ) : null}
          </>
        }
      >
        <div className='space-y-4'>
          {!release?.installable && release?.reason ? (
            <div className='flex gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm'>
              <TriangleAlertIcon className='mt-0.5 h-4 w-4 shrink-0 text-amber-500' />
              <span>{reasonLabel(release.reason)}</span>
            </div>
          ) : null}
          {release?.asset_name ? (
            <div className='bg-muted rounded-md px-3 py-2 text-sm'>
              {t('Verified update package')}: {release.asset_name}
            </div>
          ) : null}
          {release?.body ? (
            <Markdown>{release.body}</Markdown>
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t('No release notes provided.')}
            </p>
          )}
        </div>
      </Dialog>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Install this update now?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The package will be verified before installation. The service will wait for active requests and validate the new version. Automatic rollback is used only when it can be performed safely without competing with the service manager.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={installing}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleInstall} disabled={installing}>
              {installing ? (
                <LoaderCircleIcon className='me-2 h-4 w-4 animate-spin' />
              ) : (
                <DownloadIcon className='me-2 h-4 w-4' />
              )}
              {t('Start update')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={progressOpen}
        onOpenChange={(open) => {
          if (!active) setProgressOpen(open)
        }}
        showCloseButton={!active}
        title={t('Online update')}
        description={
          updateState?.target_version
            ? t('Updating from {{from}} to {{to}}', {
                from: updateState.current_version,
                to: updateState.target_version,
              })
            : undefined
        }
        contentHeight='auto'
        footer={
          !active ? (
            <>
              <Button
                type='button'
                variant='secondary'
                onClick={() => setProgressOpen(false)}
              >
                {t('Close')}
              </Button>
              {updateState?.phase === 'succeeded' ? (
                <Button type='button' onClick={() => window.location.reload()}>
                  <RefreshCcwIcon className='me-2 h-4 w-4' />
                  {t('Reload page')}
                </Button>
              ) : null}
              {updateState?.phase === 'failed' ||
              updateState?.phase === 'rolled_back' ? (
                <Button
                  type='button'
                  variant='outline'
                  onClick={handleCheckUpdates}
                >
                  <RotateCcwIcon className='me-2 h-4 w-4' />
                  {t('Check again')}
                </Button>
              ) : null}
            </>
          ) : undefined
        }
      >
        <div className='space-y-5'>
          <div className='flex items-center gap-3'>
            <UpdatePhaseIcon phase={updateState?.phase} size='lg' />
            <div>
              <div className='font-medium'>
                {phaseLabel(updateState?.phase)}
              </div>
              {reconnecting ? (
                <div className='text-muted-foreground text-sm'>
                  {t('Connection interrupted during restart. Reconnecting...')}
                </div>
              ) : null}
            </div>
          </div>
          <Progress value={updateState?.progress || 0} />
          <div className='text-muted-foreground flex justify-between text-xs'>
            <span>
              {t('Do not close the server process during replacement.')}
            </span>
            <span>{updateState?.progress || 0}%</span>
          </div>
          {updateState?.error_code ? (
            <div className='bg-destructive/10 text-destructive rounded-md p-3 text-sm'>
              {t('Error code')}: {updateState.error_code}
            </div>
          ) : null}
        </div>
      </Dialog>
    </>
  )
}
