import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { CreateLibraryRequest, LibraryAdvancedSettings } from '@/types'
import {
  AlertCircle,
  Eye,
  Film,
  FolderPlus,
  Layers,
  Plus,
  Search,
  Trash2,
  Tv,
  Video,
} from 'lucide-react'
import FileBrowser from './FileBrowser'
import {
  Button,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Select,
  Surface,
  Tag,
} from './design-system'

const LIBRARY_TYPES = [
  { value: 'movie' as const, label: '电影', desc: '各种类型电影', icon: Film },
  { value: 'tvshow' as const, label: '电视节目', desc: '电视剧、综艺等', icon: Tv },
  { value: 'mixed' as const, label: '混合影片', desc: '电影和电视节目', icon: Layers },
  { value: 'other' as const, label: '其他视频', desc: '个人视频、课程等', icon: Video },
]

const METADATA_LANG_OPTIONS = [
  { value: 'zh-CN', label: '中文简体' },
  { value: 'zh-TW', label: '中文繁體' },
  { value: 'en-US', label: 'English' },
  { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' },
  { value: 'fr', label: 'Français' },
  { value: 'de', label: 'Deutsch' },
  { value: 'es', label: 'Español' },
]

const DEFAULT_ADVANCED: LibraryAdvancedSettings = {
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

interface CreateLibraryModalProps {
  open: boolean
  onClose: () => void
  onCreate: (data: CreateLibraryRequest) => Promise<void>
}

function ToggleSwitch({
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

function FieldBlock({ label, description, children }: { label: string; description?: ReactNode; children: ReactNode }) {
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

function SettingRow({
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

export default function CreateLibraryModal({ open, onClose, onCreate }: CreateLibraryModalProps) {
  const [selectedType, setSelectedType] = useState<CreateLibraryRequest['type']>('movie')
  const [name, setName] = useState('')
  const [paths, setPaths] = useState<string[]>([''])
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [advanced, setAdvanced] = useState<LibraryAdvancedSettings>({ ...DEFAULT_ADVANCED })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [browsingIndex, setBrowsingIndex] = useState<number | null>(null)
  const nameInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setSelectedType('movie')
    setName('')
    setPaths([''])
    setShowAdvanced(false)
    setAdvanced({ ...DEFAULT_ADVANCED })
    setError('')
    setTimeout(() => nameInputRef.current?.focus(), 100)
  }, [open])

  const updateAdvanced = <K extends keyof LibraryAdvancedSettings>(
    key: K,
    value: LibraryAdvancedSettings[K],
  ) => {
    setAdvanced((prev) => ({ ...prev, [key]: value }))
  }

  const handleSubmit = async () => {
    if (!name.trim()) {
      setError('请输入媒体库名称')
      nameInputRef.current?.focus()
      return
    }

    const cleanedPaths = paths.map((path) => path.trim()).filter(Boolean)
    if (cleanedPaths.length === 0) {
      setError('请至少添加一个媒体文件夹路径')
      return
    }

    const dedupedPaths = Array.from(new Set(cleanedPaths))
    if (dedupedPaths.length !== cleanedPaths.length) {
      setError('存在重复的路径，请删除重复项')
      return
    }

    setError('')
    setSubmitting(true)
    try {
      await onCreate({
        name: name.trim(),
        paths: dedupedPaths,
        type: selectedType,
        ...advanced,
      })
      onClose()
    } catch (err: any) {
      setError(err?.response?.data?.error || '创建媒体库失败，请检查路径是否正确')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <Modal open={open} onClose={onClose} size="lg" ariaLabel="创建媒体库">
        <ModalHeader
          title="创建媒体库"
          description="选择内容类型、添加一个或多个媒体目录，并按需配置扫描与刮削策略。"
          icon={<FolderPlus size={18} />}
          onClose={onClose}
        />

        <ModalBody className="space-y-6">
          <FieldBlock label="内容类型" description="类型只影响媒体识别策略，选中状态统一使用主品牌色。">
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {LIBRARY_TYPES.map((type) => {
                const Icon = type.icon
                const selected = selectedType === type.value
                return (
                  <button
                    key={type.value}
                    type="button"
                    onClick={() => setSelectedType(type.value)}
                    className="rounded-[var(--nv-radius-card)] border p-3 text-left transition-[background-color,border-color,transform] duration-200 hover:-translate-y-0.5"
                    style={{
                      background: selected ? 'var(--nv-bg-active)' : 'var(--nv-bg-surface-soft)',
                      borderColor: selected ? 'var(--nv-action-primary)' : 'var(--nv-border-subtle)',
                    }}
                  >
                    <div
                      className="mb-3 flex h-9 w-9 items-center justify-center rounded-[var(--nv-radius-control)]"
                      style={{
                        background: selected ? 'var(--nv-ambient-cyan)' : 'var(--nv-bg-control)',
                        color: selected ? 'var(--nv-action-primary)' : 'var(--nv-text-tertiary)',
                      }}
                    >
                      <Icon size={18} />
                    </div>
                    <div className="text-sm font-semibold text-[var(--nv-text-primary)]">{type.label}</div>
                    <div className="mt-1 text-[11px] leading-4 text-[var(--nv-text-tertiary)]">{type.desc}</div>
                  </button>
                )
              })}
            </div>
          </FieldBlock>

          <FieldBlock label="媒体库名称">
            <div className="relative">
              <Input
                ref={nameInputRef}
                value={name}
                invalid={Boolean(error && !name.trim())}
                onChange={(event) => {
                  if (event.target.value.length <= 32) {
                    setName(event.target.value)
                    setError('')
                  }
                }}
                placeholder="请输入媒体库名称"
                className="pr-16"
                onKeyDown={(event) => event.key === 'Enter' && handleSubmit()}
              />
              <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs tabular-nums text-[var(--nv-text-tertiary)]">
                {name.length} / 32
              </span>
            </div>
          </FieldBlock>

          <FieldBlock
            label="媒体文件夹"
            description={
              <>
                支持多个目录，第一个路径作为主路径。例如{' '}
                <code className="rounded bg-[var(--nv-bg-control)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--nv-action-primary)]">
                  /media/movies
                </code>
                。
              </>
            }
          >
            <div className="space-y-2">
              {paths.map((path, index) => (
                <div key={index} className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    iconOnly
                    onClick={() => setBrowsingIndex(index)}
                    title="浏览服务器目录"
                  >
                    <FolderPlus size={17} />
                  </Button>
                  <Input
                    value={path}
                    invalid={Boolean(error && paths.every((item) => !item.trim()))}
                    onChange={(event) => {
                      const value = event.target.value
                      setPaths((prev) => prev.map((item, itemIndex) => (itemIndex === index ? value : item)))
                      setError('')
                    }}
                    className="flex-1"
                    placeholder={index === 0 ? '主路径，如 /media/movies 或 D:\\Videos' : '额外路径'}
                    onKeyDown={(event) => event.key === 'Enter' && handleSubmit()}
                  />
                  {paths.length > 1 && (
                    <Button
                      type="button"
                      variant="ghost"
                      iconOnly
                      onClick={() => {
                        setPaths((prev) => prev.filter((_, itemIndex) => itemIndex !== index))
                        setError('')
                      }}
                      title="移除该路径"
                      className="text-[var(--nv-status-danger)]"
                    >
                      <Trash2 size={16} />
                    </Button>
                  )}
                </div>
              ))}
            </div>
            <Button type="button" variant="ghost" size="sm" onClick={() => setPaths((prev) => [...prev, ''])}>
              <Plus size={14} />
              添加文件夹
            </Button>
          </FieldBlock>

          <Surface className="overflow-hidden rounded-[var(--nv-radius-card)] border border-[var(--nv-border-subtle)] bg-[var(--nv-bg-surface-soft)]">
            <button
              type="button"
              onClick={() => setShowAdvanced((value) => !value)}
              className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
            >
              <div>
                <div className="text-sm font-semibold text-[var(--nv-text-primary)]">高级设置</div>
                <div className="mt-1 text-xs text-[var(--nv-text-tertiary)]">扫描、元数据与文件监控策略</div>
              </div>
              <Tag tone={showAdvanced ? 'brand' : 'neutral'}>{showAdvanced ? '已展开' : '展开'}</Tag>
            </button>

            {showAdvanced && (
              <div className="space-y-5 border-t border-[var(--nv-border-subtle)] px-4 py-4">
                <SettingRow
                  title="优先读取本地 NFO 和图片"
                  description="优先读取本地 NFO 与图片，仅从互联网补充缺失信息。"
                  checked={advanced.prefer_local_nfo}
                  onChange={(value) => updateAdvanced('prefer_local_nfo', value)}
                />

                <div className="border-t border-[var(--nv-border-subtle)] pt-5">
                  <FieldBlock label="文件过滤" description="扫描时排除过小的视频文件。">
                    <div className="flex flex-wrap items-center gap-3">
                      <ToggleSwitch
                        checked={advanced.enable_file_filter}
                        onChange={(value) => updateAdvanced('enable_file_filter', value)}
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
                          updateAdvanced('min_file_size', value)
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
                      onChange={(event) => updateAdvanced('metadata_lang', event.target.value)}
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
                    onChange={(value) => updateAdvanced('auto_download_sub', value)}
                  />
                </div>

                <div className="border-t border-[var(--nv-border-subtle)] pt-5">
                  <SettingRow
                    title="扫描后自动刮削元数据"
                    description="扫描后自动解析本地 NFO 并匹配视频目录中的海报图片。"
                    icon={<Search size={15} />}
                    checked={advanced.auto_scrape_metadata}
                    onChange={(value) => updateAdvanced('auto_scrape_metadata', value)}
                  />
                </div>

                <div className="border-t border-[var(--nv-border-subtle)] pt-5">
                  <SettingRow
                    title="实时文件监控"
                    description="实时监控媒体目录变化，自动同步新增、修改或删除的文件。"
                    icon={<Eye size={15} />}
                    checked={advanced.enable_file_watch}
                    onChange={(value) => updateAdvanced('enable_file_watch', value)}
                  />
                </div>
              </div>
            )}
          </Surface>

          {error && (
            <div className="flex items-start gap-2 rounded-[var(--nv-radius-control)] border border-[var(--nv-status-danger)] bg-[var(--nv-bg-surface-soft)] px-4 py-3 text-sm text-[var(--nv-status-danger)]">
              <AlertCircle size={16} className="mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}
        </ModalBody>

        <ModalFooter>
          <Button type="button" variant="secondary" onClick={onClose} disabled={submitting}>
            取消
          </Button>
          <Button type="button" variant="primary" onClick={handleSubmit} loading={submitting}>
            {submitting ? '创建中...' : '确认创建'}
          </Button>
        </ModalFooter>
      </Modal>

      <FileBrowser
        open={browsingIndex !== null}
        onClose={() => setBrowsingIndex(null)}
        onSelect={(selectedPath) => {
          if (browsingIndex !== null) {
            setPaths((prev) => prev.map((path, index) => (index === browsingIndex ? selectedPath : path)))
            setError('')
          }
        }}
        initialPath={(browsingIndex !== null ? paths[browsingIndex] : '') || '/'}
      />
    </>
  )
}
