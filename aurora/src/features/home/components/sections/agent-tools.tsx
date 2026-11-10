/*
Agent tools showcase: the coding agents / IDE tools that work with the
gateway out of the box. Rendered as icon + name pills, mirroring the
model showcase's lobe-icons loader (silent fallback when missing).
*/
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'
import { getLobeIcon } from '@/lib/lobe-icon'

// Verified against aurora/node_modules/@lobehub/icons/es/ — `.Color` only
// where a Color variant ships.
const AGENT_TOOLS: { icon: string; name: string }[] = [
  { icon: 'Qwen.Color', name: 'Qwen Code' },
  { icon: 'Cline', name: 'Cline' },
  { icon: 'ClaudeCode.Color', name: 'Claude Code' },
  { icon: 'Cursor', name: 'Cursor' },
  { icon: 'OpenCode', name: 'OpenCode' },
  { icon: 'Codex.Color', name: 'Codex' },
  { icon: 'KiloCode', name: 'Kilo CLI' },
  { icon: 'OpenClaw.Color', name: 'OpenClaw' },
  // WorkBuddy and Coze's current mark are local assets (see CUSTOM_ICONS in
  // lib/lobe-icon).
  { icon: 'WorkBuddy', name: 'WorkBuddy' },
  { icon: 'CozeMark', name: 'Coze' },
]

export function AgentTools() {
  const { t } = useTranslation()

  return (
    <section className='relative z-10 px-6 py-20 md:py-24'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-10 text-center md:mb-14'>
          <p className='text-muted-foreground/70 mb-2 text-xs font-medium tracking-widest uppercase'>
            {t('Bring your own agent')}
          </p>
          <h2 className='text-2xl font-bold tracking-tight md:text-3xl'>
            {t('Works with your favorite agent tools')}
          </h2>
        </AnimateInView>

        <AnimateInView animation='fade-up'>
          <div className='grid grid-cols-2 gap-4 md:grid-cols-4 md:gap-5 lg:grid-cols-5'>
            {AGENT_TOOLS.map((tool, i) => (
              <AnimateInView
                key={tool.name}
                delay={i * 35}
                animation='scale-in'
                className='border-border/60 bg-background hover:border-[color:rgba(94,241,160,0.55)] hover:shadow-lg group flex items-center gap-3 rounded-2xl border px-4 py-4 shadow-sm transition-all duration-300 sm:px-5'
              >
                <span
                  role='img'
                  aria-label={tool.name}
                  title={tool.name}
                  className='shrink-0 transition-transform duration-300 group-hover:scale-110'
                >
                  {getLobeIcon(tool.icon, 28)}
                </span>
                <span className='truncate text-sm font-semibold'>
                  {tool.name}
                </span>
              </AnimateInView>
            ))}
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
