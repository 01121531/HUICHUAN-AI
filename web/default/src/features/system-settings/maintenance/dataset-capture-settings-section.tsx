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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  getDatasetCaptureModels,
  getDatasetCapturePolicy,
  updateDatasetCapturePolicy,
} from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type { DatasetCaptureModelMode, DatasetCapturePolicy } from '../types'

function createDatasetCaptureSchema(translate: (key: string) => string) {
  return z
    .object({
      enabled: z.boolean(),
      model_mode: z.enum(['all', 'selected']),
      models: z.array(z.string()),
    })
    .superRefine((value, context) => {
      if (value.model_mode === 'selected' && value.models.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['models'],
          message: translate('Select at least one model'),
        })
      }
    })
}

type DatasetCaptureFormValues = z.infer<
  ReturnType<typeof createDatasetCaptureSchema>
>

const EMPTY_POLICY: DatasetCapturePolicy = {
  enabled: false,
  model_mode: 'all',
  models: [],
}

export function DatasetCaptureSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const datasetCaptureSchema = useMemo(() => createDatasetCaptureSchema(t), [t])
  const policyQuery = useQuery({
    queryKey: ['dataset-capture-policy'],
    queryFn: getDatasetCapturePolicy,
  })
  const modelsQuery = useQuery({
    queryKey: ['dataset-capture-policy-models'],
    queryFn: getDatasetCaptureModels,
  })
  const form = useForm<DatasetCaptureFormValues>({
    resolver: zodResolver(datasetCaptureSchema),
    defaultValues: EMPTY_POLICY,
  })

  useEffect(() => {
    if (policyQuery.data?.data) {
      form.reset(policyQuery.data.data)
    }
  }, [form, policyQuery.data?.data])

  const mutation = useMutation({
    mutationFn: (policy: DatasetCapturePolicy) =>
      updateDatasetCapturePolicy(policy),
    onSuccess: (result) => {
      form.reset(result.data)
      void queryClient.invalidateQueries({
        queryKey: ['dataset-capture-policy-models'],
      })
      toast.success(t('Setting updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update setting'))
    },
  })

  const modelOptions = useMemo(
    () =>
      (modelsQuery.data?.data.models ?? []).map((model) => ({
        value: model.id,
        label: model.available ? model.id : `${model.id} (${t('Unavailable')})`,
      })),
    [modelsQuery.data?.data.models, t]
  )
  const availableModels = useMemo(
    () =>
      (modelsQuery.data?.data.models ?? [])
        .filter((model) => model.available)
        .map((model) => model.id),
    [modelsQuery.data?.data.models]
  )
  const selectedModels = form.watch('models')
  const unavailableModels = useMemo(() => {
    const available = new Set(availableModels)
    return selectedModels.filter((model) => !available.has(model))
  }, [availableModels, selectedModels])
  const selectedMode = form.watch('model_mode')

  const onSubmit = (values: DatasetCaptureFormValues) => {
    mutation.mutate(values)
  }

  if (policyQuery.isLoading || modelsQuery.isLoading) {
    return (
      <SettingsSection title={t('Dataset Capture')}>
        <div className='space-y-5'>
          <Skeleton className='h-12 w-full' />
          <Skeleton className='h-24 w-full' />
        </div>
      </SettingsSection>
    )
  }

  if (policyQuery.isError || modelsQuery.isError) {
    return (
      <SettingsSection title={t('Dataset Capture')}>
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load capture policy')}</AlertTitle>
          <AlertDescription>
            {t('Refresh the page and try again')}
          </AlertDescription>
        </Alert>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Dataset Capture')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            onReset={() => form.reset(policyQuery.data?.data ?? EMPTY_POLICY)}
            isSaving={mutation.isPending}
            isSaveDisabled={!form.formState.isDirty}
            isResetDisabled={!form.formState.isDirty}
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable dataset capture')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Capture complete successful model requests as training JSONL data'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='model_mode'
            render={({ field }) => (
              <FormItem data-settings-form-span='full'>
                <FormLabel>{t('Capture model scope')}</FormLabel>
                <FormDescription>
                  {t('Choose which requested site models can be captured')}
                </FormDescription>
                <FormControl>
                  <ToggleGroup
                    value={[field.value]}
                    onValueChange={(values) => {
                      const next = values.find((value) => value !== field.value)
                      if (next === 'all' || next === 'selected') {
                        field.onChange(next as DatasetCaptureModelMode)
                      }
                    }}
                    variant='outline'
                    spacing={0}
                    aria-label={t('Capture model scope')}
                  >
                    <ToggleGroupItem value='all'>
                      {t('All models')}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='selected'>
                      {t('Selected models')}
                    </ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {selectedMode === 'selected' && (
            <FormField
              control={form.control}
              name='models'
              render={({ field }) => (
                <FormItem data-settings-form-span='full'>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <FormLabel>{t('Site models')}</FormLabel>
                    <div className='flex items-center gap-2'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => field.onChange(availableModels)}
                      >
                        {t('Select all available models')}
                      </Button>
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        onClick={() => field.onChange([])}
                      >
                        {t('Clear')}
                      </Button>
                    </div>
                  </div>
                  <FormControl>
                    <MultiSelect
                      options={modelOptions}
                      selected={field.value}
                      onChange={field.onChange}
                      placeholder={t('Select site models...')}
                      maxVisibleChips={6}
                    />
                  </FormControl>
                  {unavailableModels.length > 0 && (
                    <p className='text-warning-foreground text-xs'>
                      {t('Unavailable selected models: {{models}}', {
                        models: unavailableModels.join(', '),
                      })}
                    </p>
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
