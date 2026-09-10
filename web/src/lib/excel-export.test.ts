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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { exportExcel, type ExcelSheet } from './excel-export'

const LOCAL_FILE_HEADER_SIGNATURE = 0x04034b50
const CENTRAL_DIRECTORY_SIGNATURE = 0x02014b50
const END_OF_CENTRAL_DIRECTORY_SIGNATURE = 0x06054b50

type ZipEntry = {
  name: string
  data: Uint8Array
  declaredChecksum: number
  compressionMethod: number
  declaredCompressedSize: number
  declaredUncompressedSize: number
}

/**
 * 独立的 ZIP 读取实现(逐位算 CRC,不复用被测代码的表驱动版本)——
 * 这个测试存在的意义就是让手写打包器被另一套实现验一遍。
 */
function readZip(bytes: Uint8Array): ZipEntry[] {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const decoder = new TextDecoder()
  const entries: ZipEntry[] = []
  let offset = 0

  while (view.getUint32(offset, true) === LOCAL_FILE_HEADER_SIGNATURE) {
    const compressionMethod = view.getUint16(offset + 8, true)
    const declaredChecksum = view.getUint32(offset + 14, true)
    const declaredCompressedSize = view.getUint32(offset + 18, true)
    const declaredUncompressedSize = view.getUint32(offset + 22, true)
    const nameLength = view.getUint16(offset + 26, true)
    const extraLength = view.getUint16(offset + 28, true)
    const dataStart = offset + 30 + nameLength + extraLength

    entries.push({
      name: decoder.decode(
        bytes.subarray(offset + 30, offset + 30 + nameLength)
      ),
      data: bytes.subarray(dataStart, dataStart + declaredCompressedSize),
      declaredChecksum,
      compressionMethod,
      declaredCompressedSize,
      declaredUncompressedSize,
    })
    offset = dataStart + declaredCompressedSize
  }

  expect(view.getUint32(offset, true)).toBe(CENTRAL_DIRECTORY_SIGNATURE)
  const centralOffset = offset
  let centralEntries = 0
  while (view.getUint32(offset, true) === CENTRAL_DIRECTORY_SIGNATURE) {
    const commentLength = view.getUint16(offset + 32, true)
    const extraLength = view.getUint16(offset + 30, true)
    const nameLength = view.getUint16(offset + 28, true)
    offset += 46 + nameLength + extraLength + commentLength
    centralEntries++
  }

  expect(view.getUint32(offset, true)).toBe(END_OF_CENTRAL_DIRECTORY_SIGNATURE)
  expect(view.getUint16(offset + 8, true)).toBe(entries.length)
  expect(view.getUint16(offset + 10, true)).toBe(centralEntries)
  expect(view.getUint32(offset + 16, true)).toBe(centralOffset)

  return entries
}

/** 逐位 CRC32,故意不查表,作为被测实现的对照。 */
function referenceCrc32(data: Uint8Array): number {
  let crc = 0xffffffff

  for (const byte of data) {
    crc ^= byte
    for (let bit = 0; bit < 8; bit++) {
      crc = crc & 1 ? (crc >>> 1) ^ 0xedb88320 : crc >>> 1
    }
  }

  return (crc ^ 0xffffffff) >>> 0
}

function decodeEntry(entry: ZipEntry): string {
  return new TextDecoder().decode(entry.data)
}

function entryByName(entries: ZipEntry[], name: string): ZipEntry {
  const entry = entries.find((candidate) => candidate.name === name)
  expect(entry, `缺少 ZIP 条目 ${name}`).toBeDefined()
  return entry as ZipEntry
}

async function buildWorkbook(sheets: ExcelSheet[]): Promise<Uint8Array> {
  let captured: Blob | undefined
  vi.spyOn(URL, 'createObjectURL').mockImplementation((blob) => {
    captured = blob as Blob
    return 'blob:test'
  })
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

  exportExcel('导出表', sheets)
  expect(captured).toBeDefined()

  return new Uint8Array(await (captured as Blob).arrayBuffer())
}

describe('exportExcel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('打包出的 xlsx 是一个 CRC 自洽的 ZIP,含全部必需的 OPC 部件', async () => {
    const bytes = await buildWorkbook([
      {
        name: '元信息',
        columns: [{ header: '模型名称', width: 32 }],
        rows: [['gpt-4o', 0.5]],
      },
    ])
    const entries = readZip(bytes)

    expect(entries.map((entry) => entry.name)).toEqual([
      '[Content_Types].xml',
      '_rels/.rels',
      'xl/workbook.xml',
      'xl/_rels/workbook.xml.rels',
      'xl/worksheets/sheet1.xml',
    ])

    for (const entry of entries) {
      expect(entry.compressionMethod).toBe(0)
      expect(entry.declaredCompressedSize).toBe(entry.declaredUncompressedSize)
      expect(entry.declaredChecksum).toBe(referenceCrc32(entry.data))
    }
  })

  it('工作表里表头、文本、数字与转义都按 SpreadsheetML 约定写出', async () => {
    const bytes = await buildWorkbook([
      {
        name: '元信息',
        columns: [
          { header: '模型名称', width: 32 },
          { header: '价格' },
          { header: '备注' },
        ],
        rows: [
          ['gpt-4o', 0.5, 'a<b>&"c"'],
          ['空值会被跳过', null, ''],
        ],
      },
    ])
    const sheet = decodeEntry(
      entryByName(readZip(bytes), 'xl/worksheets/sheet1.xml')
    )

    expect(sheet).toContain('<col min="1" max="1" width="32" customWidth="1"/>')
    expect(sheet).toContain(
      '<row r="1"><c r="A1" t="inlineStr"><is><t xml:space="preserve">模型名称</t></is></c>'
    )
    expect(sheet).toContain(
      '<c r="A2" t="inlineStr"><is><t xml:space="preserve">gpt-4o</t></is></c>'
    )
    // 数字不带 t 属性,否则 Excel 会按文本处理,排序/求和全失效。
    expect(sheet).toContain('<c r="B2"><v>0.5</v></c>')
    expect(sheet).toContain('<t xml:space="preserve">a&lt;b&gt;&amp;"c"</t>')
    // 空值整格省略(cells 按 r 寻址,稀疏行合法),不会写出空串或 null 字面量。
    expect(sheet).toContain('<row r="3"><c r="A3"')
    expect(sheet).not.toContain('null')
  })

  it('多张工作表各自独立成 part,工作表名做 Excel 硬限制清洗', async () => {
    const bytes = await buildWorkbook([
      { name: '已配置', columns: [{ header: 'A' }], rows: [] },
      {
        name: '未配置[非法]*名字/超长超长超长超长超长超长超长超长超长',
        columns: [{ header: 'A' }],
        rows: [],
      },
    ])
    const entries = readZip(bytes)
    const names = entries.map((entry) => entry.name)

    expect(names).toContain('xl/worksheets/sheet1.xml')
    expect(names).toContain('xl/worksheets/sheet2.xml')

    const workbook = decodeEntry(entryByName(entries, 'xl/workbook.xml'))
    expect(workbook).toContain('name="已配置"')
    expect(workbook).not.toContain('[非法]')
    expect(workbook).not.toContain('*')

    const sheetNames = [...workbook.matchAll(/name="([^"]*)"/g)].map(
      (match) => match[1]
    )
    for (const sheetName of sheetNames) {
      expect(sheetName.length).toBeLessThanOrEqual(31)
    }

    const contentTypes = decodeEntry(
      entryByName(entries, '[Content_Types].xml')
    )
    expect(contentTypes).toContain('/xl/worksheets/sheet2.xml')
  })

  it('下载文件名补 .xlsx 后缀并过滤 Windows 非法字符', async () => {
    const downloads: string[] = []
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
      function (this: HTMLAnchorElement) {
        downloads.push(this.download)
      }
    )

    exportExcel('模型:元信息/导出', [
      { name: 'Sheet1', columns: [{ header: 'A' }], rows: [] },
    ])
    exportExcel('already.xlsx', [
      { name: 'Sheet1', columns: [{ header: 'A' }], rows: [] },
    ])

    expect(downloads).toEqual(['模型_元信息_导出.xlsx', 'already.xlsx'])
  })

  it('没有工作表时不做任何下载', async () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL')
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    exportExcel('空表', [])

    expect(createObjectURL).not.toHaveBeenCalled()
  })
})
