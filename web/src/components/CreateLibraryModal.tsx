import { useEffect, useRef, useState } from 'react'
import type { CreateLibraryRequest, LibraryAdvancedSettings } from '@/types'
import { AlertCircle, FolderPlus, Plus, Trash2 } from 'lucide-react'
import FileBrowser from './FileBrowser'
import {
  Button,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
} from './design-system'
import {
  DEFAULT_ADVANCED,
  FieldBlock,
  LIBRARY_TYPES,
  LibraryAdvancedSettingsBlock,
  preparePaths,
} from './libraryShared'

interface CreateLibraryModalProps {
  open: boolean
  onClose: () => void
  onCreate: (data: CreateLibraryRequest) => Promise<void>
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

    const { cleaned: dedupedPaths, error: pathError } = preparePaths(paths)
    if (pathError) {
      setError(pathError)
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
    } catch (err) {
      setError(
        err && typeof err === 'object' && 'response' in err
          ? (err as { response?: { data?: { error?: string } } }).response?.data?.error || '创建媒体库失败，请检查路径是否正确'
          : '创建媒体库失败，请检查路径是否正确',
      )
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

          <LibraryAdvancedSettingsBlock
            open={showAdvanced}
            onToggle={() => setShowAdvanced((value) => !value)}
            advanced={advanced}
            onChange={updateAdvanced}
          />

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