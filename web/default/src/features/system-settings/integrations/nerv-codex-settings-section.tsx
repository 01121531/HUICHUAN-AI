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
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const nervCodexSchema = z
  .object({
    nerv_setting: z.object({
      enabled: z.boolean(),
      prompt: z.string().max(20000),
      mode: z.enum(['prepend', 'append', 'override']),
      models: z.string(),
    }),
  })
  .refine(
    (values) =>
      !values.nerv_setting.enabled ||
      values.nerv_setting.prompt.trim().length > 0,
    {
      path: ['nerv_setting', 'prompt'],
      message: '\u542f\u7528\u540e\u5fc5\u987b\u586b\u5199\u6865\u63a5\u63d0\u793a\u8bcd',
    }
  )

type NERVCodexFormInput = z.input<typeof nervCodexSchema>
type NERVCodexFormValues = z.output<typeof nervCodexSchema>

type FlatNERVCodexDefaults = {
  'nerv_setting.enabled': boolean
  'nerv_setting.prompt': string
  'nerv_setting.mode': 'prepend' | 'append' | 'override'
  'nerv_setting.models': string
}

type NERVCodexSettingsSectionProps = {
  defaultValues: FlatNERVCodexDefaults
}

const buildFormDefaults = (
  defaults: FlatNERVCodexDefaults
): NERVCodexFormInput => ({
  nerv_setting: {
    enabled: defaults['nerv_setting.enabled'],
    prompt: defaults['nerv_setting.prompt'] ?? '',
    mode: defaults['nerv_setting.mode'] ?? 'prepend',
    models: defaults['nerv_setting.models'] ?? 'gpt-5.6*,codex*',
  },
})

const normalizeFormValues = (
  values: NERVCodexFormValues
): FlatNERVCodexDefaults => ({
  'nerv_setting.enabled': values.nerv_setting.enabled,
  'nerv_setting.prompt': values.nerv_setting.prompt.trim(),
  'nerv_setting.mode': values.nerv_setting.mode,
  'nerv_setting.models': values.nerv_setting.models.trim(),
})

export function NERVCodexSettingsSection({
  defaultValues,
}: NERVCodexSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<NERVCodexFormInput, unknown, NERVCodexFormValues>({
    resolver: zodResolver(nervCodexSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (values: NERVCodexFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = Object.entries(normalized).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof FlatNERVCodexDefaults]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  return (
    <SettingsSection title={t('NERV \u8fde\u63a5\u8bbe\u7f6e')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onReset={() => form.reset(formDefaults)}
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            resetLabel='\u6062\u590d\u9ed8\u8ba4'
            saveLabel='\u4fdd\u5b58\u8bbe\u7f6e'
          />

          <Alert>
            <AlertDescription>
              {t('NERV \u8fde\u63a5\u914d\u7f6e\uff1a\u5bf9\u5339\u914d\u7684 Codex Responses \u8bf7\u6c42\u6ce8\u5165\u6865\u63a5\u63d0\u793a\u8bcd\u3002')}
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='nerv_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('\u542f\u7528 NERV \u8fde\u63a5')}</FormLabel>
                  <FormDescription>
                  {t('\u5f00\u542f\u540e\u4f1a\u5bf9\u5339\u914d\u6a21\u578b\u6ce8\u5165\u6865\u63a5\u63d0\u793a\u8bcd\u3002')}
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
            name='nerv_setting.mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('\u6ce8\u5165\u6a21\u5f0f')}</FormLabel>
                <FormControl>
                  <NativeSelect
                    value={field.value}
                    onChange={(event) => field.onChange(event.target.value)}
                  >
                    <NativeSelectOption value='prepend'>
                      {t('\u653e\u5728\u539f\u6709\u6307\u4ee4\u524d\u9762')}
                    </NativeSelectOption>
                    <NativeSelectOption value='append'>
                      {t('\u8ffd\u52a0\u5230\u539f\u6709\u6307\u4ee4\u540e\u9762')}
                    </NativeSelectOption>
                    <NativeSelectOption value='override'>
                      {t('\u8986\u76d6\u539f\u6709\u6307\u4ee4')}
                    </NativeSelectOption>
                  </NativeSelect>
                </FormControl>
                <FormDescription>
                  {t('\u9009\u62e9\u6865\u63a5\u63d0\u793a\u8bcd\u4e0e\u539f\u6709\u7cfb\u7edf\u6307\u4ee4\u7684\u7ec4\u5408\u65b9\u5f0f\u3002')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='nerv_setting.models'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('\u6a21\u578b\u5339\u914d\u89c4\u5219')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='gpt-5.6*,codex*'
                    autoComplete='off'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('\u7528\u9017\u53f7\u3001\u5206\u53f7\u6216\u6362\u884c\u5206\u9694\uff0c* \u8868\u793a\u901a\u914d\u3002')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='nerv_setting.prompt'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('\u6865\u63a5\u63d0\u793a\u8bcd')}</FormLabel>
                <FormControl>
                  <Textarea
                    className='min-h-48 font-mono text-xs'
                    placeholder={t('\u8bf7\u8f93\u5165 NERV \u6865\u63a5\u63d0\u793a\u8bcd')}
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('\u4ec5\u5728\u542f\u7528\u540e\u6ce8\u5165\u5339\u914d\u7684 Codex \u8bf7\u6c42\u3002')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
