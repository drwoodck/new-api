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
import { useTranslation } from 'react-i18next'

import { defaultAuth } from './codec'
import { FieldRow, KindSelect, TextField } from './field-primitives'
import { AUTH_METHODS, type Auth, type AuthMethod } from './types'

export function AuthEditor({
  value,
  onChange,
}: {
  value: Auth
  onChange: (next: Auth) => void
}) {
  const { t } = useTranslation()
  const labels: Record<AuthMethod, string> = {
    bearer: t('Bearer Token'),
    header: t('自定义 Header'),
    query_param: t('URL 查询参数'),
  }

  return (
    <div className='space-y-2'>
      <KindSelect
        value={value.method}
        options={AUTH_METHODS}
        labels={labels}
        onChange={(method) => onChange(defaultAuth(method))}
      />
      {value.method === 'header' && (
        <FieldRow label={t('Header 名称')}>
          <TextField
            value={value.name}
            placeholder='X-API-Key'
            onChange={(name) => onChange({ ...value, name })}
          />
        </FieldRow>
      )}
      {value.method === 'query_param' && (
        <FieldRow label={t('查询参数名')}>
          <TextField
            value={value.key}
            placeholder='api_key'
            onChange={(key) => onChange({ ...value, key })}
          />
        </FieldRow>
      )}
    </div>
  )
}
