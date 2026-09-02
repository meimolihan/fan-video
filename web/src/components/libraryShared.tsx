import type { ReactNode } from 'react'
import { Eye, Film, Layers, Search, Tv, Video } from 'lucide-react'
import type { LibraryAdvancedSettings } from '@/types'
import { Input, Select, Surface, Tag } from './design-system'

export const LIBRARY_TYPES = [
  { value: 'movie' as const, label: '视频', desc: '各种类型视频', icon: Film },
  { value: 'tvshow' as const, label: '电视节目', desc: '电视剧、综艺等', icon: Tv },
  { value: 'mixed' as const, label: '混合影片', desc: '视频和电视节目', icon: Layers },
  { value: 'other' as const, label: '其他视频', desc: '个人视频、课程等', icon: Video },
]

export const METADATA_LANG_OPTIONS = [
  { value: 'zh-CN', label: '中文简体' },
  { value: 'zh-TW', label: '中文繁體' },
  { value: 'en-US', label: 'English' },
  { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' },
  { value: 'fr', label: 'Français' },
  { value: 'de', label: 'Deutsch' },
  { value: 'es', label: 'Español' },
]

export const DEFAULT_ADVANCED: LibraryAdvancedSettings = {
  prefer_local_nfo: true,
  enable_file_filter: true,
  min_file_size: 3,
  metadata_lang: 'zh-CN',
  allow_adult_content: false,
  auto_download_sub: false,
  auto_scrape_metadata: true,
  auto_organize_mode: 'off',
  organize_output_dir: '',
  enable_file_watch: false,
}

export function ToggleSwitch({
  checked,
  onChange,
  disabled,
}: {
  checked: boolean
  onChange: (value: boolean) => void
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className="relative inline-flex h-6 w-11 shrink-0 rounded-full border transition-colors duration-200 focus:outline-none focus-visible:shadow-[var(--nv-shadow-focus)] disabled:cursor-not-allowed disabled:opacity-50"
      style={{
        background: checked ? 'var(--nv-action-primary)' : 'var(--nv-bg-control)',
        borderColor: checked ? 'var(--nv-action-primary)' : 'var(--nv-border-default)',
      }}
    >
      <span
        className="absolute top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform duration-200"
        style={{ transform: checked ? 'translateX(20px)' : 'translateX(2px)' }}
      />
    </button>
  )
}

export function FieldBlock({ label, description, children }: { label: string; description?: ReactNode; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <div>
        <div className="text-sm font-medium text-[var(--nv-text-primary)]">{label}</div>
        {description && (
          <div className="mt-1 text-xs leading-5 text-[var(--nv-text-tertiary)]">{description}</div>
        )}
      </div>
      {children}
    </div>
  )
}

export function SettingRow({
  title,
  description,
  icon,
  checked,
  onChange,
}: {
  title: string
  description: ReactNode
  icon?: ReactNode
  checked: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4 py-1">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-sm font-medium text-[var(--nv-text-primary)]">
          {icon && <span className="text-[var(--nv-action-primary)]">{icon}</span>}
          {title}
        </div>
        <div className="mt-1 text-xs leading-5 text-[var(--nv-text-tertiary)]">{description}</div>
      </div>
      <ToggleSwitch checked={checked} onChange={onChange} />
    </div>
  )
}

// LibraryAdvancedSettingsBlock 媒体库高级设置折叠区（扫描 / 元数据 / 文件监控策略）。
export function LibraryAdvancedSettingsBlock({
  open,
  onToggle,
  advanced,
  onChange,
}: {
  open: boolean
  onToggle: () => void
  advanced: LibraryAdvancedSettings
  onChange: <K extends keyof LibraryAdvancedSettings>(key: K, value: LibraryAdvancedSettings[K]) => void
}) {
  return (
    <Surface className="overflow-hidden rounded-[var(--nv-radius-card)] border border-[var(--nv-border-subtle)] bg-[var(--nv-bg-surface-soft)]">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
      >
        <div>
          <div className="text-sm font-semibold text-[var(--nv-text-primary)]">高级设置</div>
          <div className="mt-1 text-xs text-[var(--nv-text-tertiary)]">扫描、元数据与文件监控策略</div>
        </div>
        <Tag tone={open ? 'brand' : 'neutral'}>{open ? '已展开' : '展开'}</Tag>
      </button>

      {open && (
        <div className="space-y-5 border-t border-[var(--nv-border-subtle)] px-4 py-4">
          <SettingRow
            title="优先读取本地 NFO 和图片"
            description="优先读取本地 NFO 与图片，仅从互联网补充缺失信息。"
            checked={advanced.prefer_local_nfo}
            onChange={(value) => onChange('prefer_local_nfo', value)}
          />

          <div className="border-t border-[var(--nv-border-subtle)] pt-5">
            <FieldBlock label="文件过滤" description="扫描时排除过小的视频文件。">
              <div className="flex flex-wrap items-center gap-3">
                <ToggleSwitch
                  checked={advanced.enable_file_filter}
                  onChange={(value) => onChange('enable_file_filter', value)}
                />
                <span className="text-sm text-[var(--nv-text-secondary)]">排除小于</span>
                <Input
                  type="number"
                  min={0}
                  max={999}
                  value={advanced.min_file_size}
                  disabled={!advanced.enable_file_filter}
                  onChange={(event) => {
                    const value = Math.max(0, Math.min(999, Number.parseInt(event.target.value) || 0))
                    onChange('min_file_size', value)
                  }}
                  className="w-24 text-center tabular-nums"
                />
                <span className="text-sm text-[var(--nv-text-secondary)]">MB 的视频文件</span>
              </div>
            </FieldBlock>
          </div>

          <div className="border-t border-[var(--nv-border-subtle)] pt-5">
            <FieldBlock label="媒体元数据下载语言" description="优先使用所选语言下载影片、演员与海报信息。">
              <Select
                value={advanced.metadata_lang}
                onChange={(event) => onChange('metadata_lang', event.target.value as LibraryAdvancedSettings['metadata_lang'])}
                className="w-full"
              >
                {METADATA_LANG_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </Select>
            </FieldBlock>
          </div>

          <div className="border-t border-[var(--nv-border-subtle)] pt-5">
            <SettingRow
              title="自动下载字幕"
              description="对未内嵌字幕的媒体文件，自动从互联网下载字幕。"
              checked={advanced.auto_download_sub}
              onChange={(value) => onChange('auto_download_sub', value)}
            />
          </div>

          <div className="border-t border-[var(--nv-border-subtle)] pt-5">
            <SettingRow
              title="扫描后自动刮削元数据"
              description="扫描后自动解析本地 NFO 并匹配视频目录中的海报图片。"
              icon={<Search size={15} />}
              checked={advanced.auto_scrape_metadata}
              onChange={(value) => onChange('auto_scrape_metadata', value)}
            />
          </div>

          <div className="border-t border-[var(--nv-border-subtle)] pt-5">
            <SettingRow
              title="实时文件监控"
              description="实时监控媒体目录变化，自动同步新增、修改或删除的文件。"
              icon={<Eye size={15} />}
              checked={advanced.enable_file_watch}
              onChange={(value) => onChange('enable_file_watch', value)}
            />
          </div>
        </div>
      )}
    </Surface>
  )
}

// preparePaths 清洗并去重媒体文件夹路径；返回 ({ cleaned, error })。
export function preparePaths(paths: string[]): { cleaned: string[]; error: string } {
  const cleaned = Array.from(new Set(paths.map((path) => path.trim()).filter(Boolean)))
  const raw = paths.map((path) => path.trim()).filter(Boolean)
  if (raw.length === 0) return { cleaned, error: '请至少添加一个媒体文件夹路径' }
  if (cleaned.length !== raw.length) return { cleaned, error: '存在重复的路径，请删除重复项' }
  return { cleaned, error: '' }
}