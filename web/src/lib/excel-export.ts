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
/**
 * 零依赖的最小 XLSX 写出器。
 *
 * xlsx 本质是一个 OPC 包(即 ZIP),这里用 ZIP 的 store(不压缩)方式打包,
 * 因此只需要 CRC32 和本地文件头/中央目录,不需要任何压缩库。不引第三方
 * 表格库的理由:导出只是把已经渲染好的列原样落盘,为此背一个几百 KB、
 * 且历史上拿过解析类 CVE 的依赖并不划算。store 方式体积偏大,但导出量级
 * 下无感,Excel / WPS / LibreOffice 都能直接打开。
 *
 * 字符串一律写成 inlineStr,不建 sharedStrings 表 —— 导出通常没有重复值,
 * 省掉一层间接反而更简单。
 */

export type ExcelCellValue = string | number | boolean | null | undefined

export type ExcelColumn = {
  header: string
  /** 列宽,单位约等于字符数。省略时用 Excel 默认宽度。 */
  width?: number
}

export type ExcelSheet = {
  name: string
  columns: ExcelColumn[]
  rows: ExcelCellValue[][]
}

const ZIP_LOCAL_FILE_HEADER_SIGNATURE = 0x04034b50
const ZIP_CENTRAL_DIRECTORY_SIGNATURE = 0x02014b50
const ZIP_END_OF_CENTRAL_DIRECTORY_SIGNATURE = 0x06054b50
/** 2.0 —— store 方式不需要更高的解压能力。 */
const ZIP_VERSION = 20
/** 置位表示文件名按 UTF-8 解释。 */
const ZIP_FLAG_UTF8 = 0x0800
const ZIP_METHOD_STORE = 0
const ZIP_LOCAL_HEADER_SIZE = 30
const ZIP_CENTRAL_ENTRY_SIZE = 46
const ZIP_END_OF_CENTRAL_DIRECTORY_SIZE = 22
/** ZIP 32 位长度字段的上限,超过必须改用 ZIP64。 */
const ZIP32_SIZE_LIMIT = 0xffffffff

const SPREADSHEETML_NAMESPACE =
  'http://schemas.openxmlformats.org/spreadsheetml/2006/main'
const DOCUMENT_RELATIONSHIP_NAMESPACE =
  'http://schemas.openxmlformats.org/officeDocument/2006/relationships'
const PACKAGE_RELATIONSHIP_NAMESPACE =
  'http://schemas.openxmlformats.org/package/2006/relationships'
const CONTENT_TYPES_NAMESPACE =
  'http://schemas.openxmlformats.org/package/2006/content-types'
const XLSX_MIME_TYPE =
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'

/** Excel 对工作表名的硬限制:31 字符,且不允许 \ / ? * [ ] : */
const MAX_SHEET_NAME_LENGTH = 31
const INVALID_SHEET_NAME_CHARS = /[\\/?*[\]:]/g
/** Windows 文件名非法字符(导出目标多为 Windows 桌面 Excel)。 */
const INVALID_FILE_NAME_CHARS = /[\\/:*?"<>|]/g
/** XML 1.0 不允许的 C0 控制字符(除 \t \n \r)—— 留着会让 Excel 判文件损坏。 */
// eslint-disable-next-line no-control-regex
const ILLEGAL_XML_CHARS = /[\u0000-\u0008\u000B\u000C\u000E-\u001F]/g

export function exportExcel(fileName: string, sheets: ExcelSheet[]): void {
  if (sheets.length === 0) return

  const bytes = buildWorkbookFile(sheets)
  const blob = new Blob([bytes as unknown as BlobPart], {
    type: XLSX_MIME_TYPE,
  })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')

  anchor.href = url
  anchor.download = toFileName(fileName)
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  // 立刻 revoke 会在部分浏览器里把刚开始的下载一起掐掉,放到下一个宏任务。
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

function toFileName(fileName: string): string {
  const sanitized = fileName.replace(INVALID_FILE_NAME_CHARS, '_').trim()
  const base = sanitized === '' ? 'export' : sanitized
  return base.toLowerCase().endsWith('.xlsx') ? base : `${base}.xlsx`
}

function buildWorkbookFile(sheets: ExcelSheet[]): Uint8Array {
  const sheetPaths = sheets.map(
    (_, index) => `xl/worksheets/sheet${index + 1}.xml`
  )

  const files: ZipEntryInput[] = [
    {
      name: '[Content_Types].xml',
      data: encodeXml(buildContentTypesXml(sheets.length)),
    },
    { name: '_rels/.rels', data: encodeXml(buildRootRelationshipsXml()) },
    { name: 'xl/workbook.xml', data: encodeXml(buildWorkbookXml(sheets)) },
    {
      name: 'xl/_rels/workbook.xml.rels',
      data: encodeXml(buildWorkbookRelationshipsXml(sheetPaths)),
    },
  ]

  sheets.forEach((sheet, index) => {
    files.push({
      name: sheetPaths[index],
      data: encodeXml(buildWorksheetXml(sheet)),
    })
  })

  return buildZip(files)
}

function buildContentTypesXml(sheetCount: number): string {
  const overrides = Array.from(
    { length: sheetCount },
    (_, index) =>
      `<Override PartName="/xl/worksheets/sheet${index + 1}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`
  ).join('')

  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="${CONTENT_TYPES_NAMESPACE}"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>${overrides}</Types>`
}

function buildRootRelationshipsXml(): string {
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${PACKAGE_RELATIONSHIP_NAMESPACE}"><Relationship Id="rId1" Type="${DOCUMENT_RELATIONSHIP_NAMESPACE}/officeDocument" Target="xl/workbook.xml"/></Relationships>`
}

function buildWorkbookXml(sheets: ExcelSheet[]): string {
  const sheetTags = sheets
    .map(
      (sheet, index) =>
        `<sheet name="${escapeXmlAttribute(toSheetName(sheet.name, index))}" sheetId="${index + 1}" r:id="rId${index + 1}"/>`
    )
    .join('')

  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="${SPREADSHEETML_NAMESPACE}" xmlns:r="${DOCUMENT_RELATIONSHIP_NAMESPACE}"><sheets>${sheetTags}</sheets></workbook>`
}

function buildWorkbookRelationshipsXml(sheetPaths: string[]): string {
  const relationships = sheetPaths
    .map(
      (path, index) =>
        `<Relationship Id="rId${index + 1}" Type="${DOCUMENT_RELATIONSHIP_NAMESPACE}/worksheet" Target="${path.replace('xl/', '')}"/>`
    )
    .join('')

  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${PACKAGE_RELATIONSHIP_NAMESPACE}">${relationships}</Relationships>`
}

function buildWorksheetXml(sheet: ExcelSheet): string {
  const cols = sheet.columns
    .map((column, index) =>
      typeof column.width === 'number' && column.width > 0
        ? `<col min="${index + 1}" max="${index + 1}" width="${column.width}" customWidth="1"/>`
        : ''
    )
    .join('')

  const headerRow = buildRowXml(
    1,
    sheet.columns.map((column) => column.header)
  )
  const bodyRows = sheet.rows
    .map((row, index) => buildRowXml(index + 2, row))
    .join('')

  const colsTag = cols === '' ? '' : `<cols>${cols}</cols>`

  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="${SPREADSHEETML_NAMESPACE}">${colsTag}<sheetData>${headerRow}${bodyRows}</sheetData></worksheet>`
}

function buildRowXml(rowNumber: number, cells: ExcelCellValue[]): string {
  const cellTags = cells
    .map((value, index) => buildCellXml(rowNumber, index, value))
    .join('')

  return `<row r="${rowNumber}">${cellTags}</row>`
}

function buildCellXml(
  rowNumber: number,
  columnIndex: number,
  value: ExcelCellValue
): string {
  const reference = `${toColumnName(columnIndex)}${rowNumber}`

  if (value === null || value === undefined || value === '') return ''
  if (typeof value === 'boolean') {
    return `<c r="${reference}" t="b"><v>${value ? 1 : 0}</v></c>`
  }
  if (typeof value === 'number') {
    // Excel 的数字格式不接受 NaN / Infinity,落回文本比写出一个坏单元格好。
    if (Number.isFinite(value)) {
      return `<c r="${reference}"><v>${value}</v></c>`
    }
  }

  return `<c r="${reference}" t="inlineStr"><is><t xml:space="preserve">${escapeXmlText(String(value))}</t></is></c>`
}

/** 0 → A、25 → Z、26 → AA。 */
function toColumnName(columnIndex: number): string {
  let name = ''
  let remaining = columnIndex

  while (remaining >= 0) {
    name = String.fromCharCode(65 + (remaining % 26)) + name
    remaining = Math.floor(remaining / 26) - 1
  }

  return name
}

function toSheetName(name: string, index: number): string {
  const sanitized = name.replace(INVALID_SHEET_NAME_CHARS, '_').trim()
  if (sanitized === '') return `Sheet${index + 1}`
  return sanitized.slice(0, MAX_SHEET_NAME_LENGTH)
}

function escapeXmlText(value: string): string {
  return value
    .replaceAll(ILLEGAL_XML_CHARS, '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
}

function escapeXmlAttribute(value: string): string {
  return escapeXmlText(value).replaceAll('"', '&quot;')
}

function encodeXml(xml: string): Uint8Array {
  return new TextEncoder().encode(xml)
}

type ZipEntryInput = {
  name: string
  data: Uint8Array
}

function buildZip(entries: ZipEntryInput[]): Uint8Array {
  const encoder = new TextEncoder()
  const { time, date } = toDosTimestamp()
  const localChunks: Uint8Array[] = []
  const centralChunks: Uint8Array[] = []
  let offset = 0

  for (const entry of entries) {
    if (entry.data.length > ZIP32_SIZE_LIMIT) {
      throw new Error('导出内容超过单个 xlsx 条目上限(4GB)')
    }

    const nameBytes = encoder.encode(entry.name)
    const checksum = crc32(entry.data)
    const local = new Uint8Array(ZIP_LOCAL_HEADER_SIZE + nameBytes.length)
    const localView = new DataView(local.buffer)

    localView.setUint32(0, ZIP_LOCAL_FILE_HEADER_SIGNATURE, true)
    localView.setUint16(4, ZIP_VERSION, true)
    localView.setUint16(6, ZIP_FLAG_UTF8, true)
    localView.setUint16(8, ZIP_METHOD_STORE, true)
    localView.setUint16(10, time, true)
    localView.setUint16(12, date, true)
    localView.setUint32(14, checksum, true)
    localView.setUint32(18, entry.data.length, true)
    localView.setUint32(22, entry.data.length, true)
    localView.setUint16(26, nameBytes.length, true)
    localView.setUint16(28, 0, true)
    local.set(nameBytes, ZIP_LOCAL_HEADER_SIZE)

    const central = new Uint8Array(ZIP_CENTRAL_ENTRY_SIZE + nameBytes.length)
    const centralView = new DataView(central.buffer)

    centralView.setUint32(0, ZIP_CENTRAL_DIRECTORY_SIGNATURE, true)
    centralView.setUint16(4, ZIP_VERSION, true)
    centralView.setUint16(6, ZIP_VERSION, true)
    centralView.setUint16(8, ZIP_FLAG_UTF8, true)
    centralView.setUint16(10, ZIP_METHOD_STORE, true)
    centralView.setUint16(12, time, true)
    centralView.setUint16(14, date, true)
    centralView.setUint32(16, checksum, true)
    centralView.setUint32(20, entry.data.length, true)
    centralView.setUint32(24, entry.data.length, true)
    centralView.setUint16(28, nameBytes.length, true)
    centralView.setUint16(30, 0, true)
    centralView.setUint16(32, 0, true)
    centralView.setUint16(34, 0, true)
    centralView.setUint16(36, 0, true)
    centralView.setUint32(38, 0, true)
    centralView.setUint32(42, offset, true)
    central.set(nameBytes, ZIP_CENTRAL_ENTRY_SIZE)

    localChunks.push(local, entry.data)
    centralChunks.push(central)
    offset += local.length + entry.data.length
  }

  const centralSize = centralChunks.reduce(
    (total, chunk) => total + chunk.length,
    0
  )
  const end = new Uint8Array(ZIP_END_OF_CENTRAL_DIRECTORY_SIZE)
  const endView = new DataView(end.buffer)

  endView.setUint32(0, ZIP_END_OF_CENTRAL_DIRECTORY_SIGNATURE, true)
  endView.setUint16(4, 0, true)
  endView.setUint16(6, 0, true)
  endView.setUint16(8, entries.length, true)
  endView.setUint16(10, entries.length, true)
  endView.setUint32(12, centralSize, true)
  endView.setUint32(16, offset, true)
  endView.setUint16(20, 0, true)

  return concatBytes([...localChunks, ...centralChunks, end])
}

function toDosTimestamp(): { time: number; date: number } {
  const now = new Date()
  const year = now.getFullYear()

  // DOS 时间戳从 1980 年起算,且秒只有 2 秒精度。
  if (year < 1980) {
    return { time: 0, date: (1 << 5) | 1 }
  }

  return {
    time:
      (now.getHours() << 11) |
      (now.getMinutes() << 5) |
      Math.floor(now.getSeconds() / 2),
    date: ((year - 1980) << 9) | ((now.getMonth() + 1) << 5) | now.getDate(),
  }
}

function concatBytes(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0)
  const out = new Uint8Array(total)
  let offset = 0

  for (const chunk of chunks) {
    out.set(chunk, offset)
    offset += chunk.length
  }

  return out
}

const CRC32_TABLE = buildCrc32Table()

function buildCrc32Table(): Uint32Array {
  const table = new Uint32Array(256)

  for (let index = 0; index < 256; index++) {
    let value = index
    for (let bit = 0; bit < 8; bit++) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1
    }
    table[index] = value >>> 0
  }

  return table
}

/** 标准 IEEE 802.3 CRC32,与 ZIP 条目校验字段一致。 */
function crc32(data: Uint8Array): number {
  let crc = 0xffffffff

  for (let index = 0; index < data.length; index++) {
    crc = (crc >>> 8) ^ CRC32_TABLE[(crc ^ data[index]) & 0xff]
  }

  return (crc ^ 0xffffffff) >>> 0
}
