import { useEffect, useRef, useState } from 'react'
import { Check, Image } from 'lucide-react'
import type { Media } from '@/types'
import { streamApi } from '@/api'
import { Button } from '@/components/design-system'
import { Modal, ModalBody, ModalFooter, ModalHeader } from '@/components/design-system/Modal'
import PosterImage from '@/components/PosterImage'

interface SeriesPosterPickerModalProps {
  open: boolean
  episodes: Media[]
  seriesId: string
  currentPosterVersion: number
  onConfirm: (mediaId: string) => void | Promise<void>
  onClose: () => void
}

function episodeCode(episode: Media) {
  return episode.episode_title || episode.title
}

export default function SeriesPosterPickerModal({
  open,
  episodes,
  seriesId: _seriesId,
  currentPosterVersion,
  onConfirm,
  onClose,
}: SeriesPosterPickerModalProps) {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const prevOpenRef = useRef(open)

  useEffect(() => {
    if (open && !prevOpenRef.current) {
      setSelectedId(null)
      setLoading(false)
    }
    prevOpenRef.current = open
  }, [open])

  const sortedEpisodes = [...episodes]
    .filter((ep) => ep.poster_path)
    .sort((a, b) => a.season_num - b.season_num || a.episode_num - b.episode_num)

  const handleConfirm = async () => {
    if (!selectedId) return
    setLoading(true)
    try {
      await onConfirm(selectedId)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} size="lg" ariaLabel="选择剧集海报">
      <ModalHeader
        title="选择剧集海报"
        description="选择一张影视海报作为剧集合集封面。"
        icon={<Image size={18} aria-hidden="true" />}
        onClose={onClose}
      />
      <ModalBody>
        {sortedEpisodes.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-10 text-sm text-[var(--nv-text-tertiary)]">
            <Image size={28} className="mb-2 opacity-40" aria-hidden="true" />
            <p>该分组下暂无影视海报</p>
          </div>
        ) : (
          <div className="nv-poster-picker-grid grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6">
            {sortedEpisodes.map((episode) => {
              const isSelected = selectedId === episode.id
              return (
                <button
                  key={episode.id}
                  type="button"
                  onClick={() => setSelectedId(episode.id)}
                  className={`nv-poster-picker-item group relative flex flex-col items-center gap-1.5 rounded-[var(--nv-radius-card)] p-1.5 transition-all ${isSelected ? 'ring-2 ring-[var(--nv-accent)] bg-[var(--nv-accent)]/10' : 'hover:bg-[var(--nv-bg-subtle)]'}`}
                >
                  <div className="relative w-full overflow-hidden rounded-[var(--nv-radius-card)] bg-[var(--nv-bg-subtle)]" style={{ aspectRatio: '2/3' }}>
                    <PosterImage
                      src={streamApi.getPosterUrl(episode.id, currentPosterVersion)}
                      alt={episode.title}
                      className="h-full w-full object-cover"
                    />
                    {isSelected && (
                      <div className="absolute inset-0 flex items-center justify-center bg-[var(--nv-accent)]/20">
                        <div className="flex h-7 w-7 items-center justify-center rounded-full bg-[var(--nv-accent)] text-white shadow-md">
                          <Check size={15} strokeWidth={2.5} aria-hidden="true" />
                        </div>
                      </div>
                    )}
                  </div>
                  <span className="w-full truncate text-center text-[10px] leading-tight text-[var(--nv-text-secondary)]">
                    {episodeCode(episode)}
                  </span>
                </button>
              )
            })}
          </div>
        )}
      </ModalBody>
      <ModalFooter>
        <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>取消</Button>
        <Button type="button" variant="primary" onClick={handleConfirm} disabled={!selectedId} loading={loading}>
          确认更换
        </Button>
      </ModalFooter>
    </Modal>
  )
}
