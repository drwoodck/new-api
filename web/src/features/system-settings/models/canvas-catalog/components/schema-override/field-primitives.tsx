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
import type { ReactNode } from 'react'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

export function FieldRow({
  label,
  required,
  description,
  children,
  className,
}: {
  label: string
  required?: boolean
  description?: string
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cn('grid gap-1.5', className)}>
      <Label>
        {label}
        {required ? ' *' : ''}
      </Label>
      {children}
      {description ? (
        <p className='text-muted-foreground text-xs'>{description}</p>
      ) : null}
    </div>
  )
}

export function KindSelect<T extends string>({
  value,
  options,
  labels,
  onChange,
  className,
}: {
  value: T
  options: readonly T[]
  labels?: Partial<Record<T, string>>
  onChange: (next: T) => void
  className?: string
}) {
  return (
    <Select<T>
      items={options.map((opt) => ({ value: opt, label: labels?.[opt] ?? opt }))}
      value={value}
      onValueChange={(v) => v !== null && onChange(v)}
    >
      <SelectTrigger size='sm' className={cn('w-full sm:w-[220px]', className)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {options.map((opt) => (
            <SelectItem key={opt} value={opt}>
              {labels?.[opt] ?? opt}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

export function StringListTextarea({
  value,
  onChange,
  placeholder,
  rows = 2,
}: {
  value: string[]
  onChange: (next: string[]) => void
  placeholder?: string
  rows?: number
}) {
  return (
    <Textarea
      rows={rows}
      value={value.join('\n')}
      placeholder={placeholder}
      onChange={(e) =>
        onChange(
          e.target.value
            .split('\n')
            .map((s) => s.trim())
            .filter((s) => s.length > 0)
        )
      }
    />
  )
}

export function TextField({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string
  onChange: (next: string) => void
  placeholder?: string
  className?: string
}) {
  return (
    <Input
      className={className}
      value={value}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

export function NumberField({
  value,
  onChange,
  placeholder,
  min,
  className,
}: {
  value: number
  onChange: (next: number) => void
  placeholder?: string
  min?: number
  className?: string
}) {
  return (
    <Input
      type='number'
      min={min}
      className={className}
      value={Number.isFinite(value) ? value : ''}
      placeholder={placeholder}
      onChange={(e) => {
        const next = e.target.valueAsNumber
        if (Number.isFinite(next)) onChange(next)
      }}
    />
  )
}
