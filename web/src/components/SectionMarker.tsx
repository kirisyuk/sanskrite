import React from 'react';

interface SectionMarkerProps {
  interaction: any;
  collapsedSections: Record<string, boolean>;
  onToggleCollapse: (sectionId: string) => void;
}

export const SectionMarker: React.FC<SectionMarkerProps> = ({
  interaction,
  collapsedSections,
  onToggleCollapse,
}) => {
  const sid = (interaction.sectionId || interaction.id) as string;

  if (interaction.kind === 'section_start') {
    const collapsed = !!collapsedSections[sid];
    return (
      <div
        key={interaction.id}
        data-interaction-id={interaction.id}
        className="my-2 sticky top-0 z-10"
        style={{
          background: 'var(--surface-700)',
          border: '1px dashed var(--accent-research)',
          borderRadius: 10,
          padding: '6px 8px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <div
          className="text-12 font-medium"
          style={{
            color: 'var(--text-heading)',
          }}
        >
          {interaction.content || 'Section'}
        </div>
        <button
          type="button"
          className="px-2 py-1 rounded text-11"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
          }}
          onClick={() => onToggleCollapse(sid)}
          aria-label={collapsed ? 'Expand section' : 'Collapse section'}
          title={collapsed ? 'Expand section' : 'Collapse section'}
        >
          {collapsed ? 'Expand' : 'Collapse'}
        </button>
      </div>
    );
  }

  // section_end divider
  return (
    <div
      key={interaction.id}
      className="my-2"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
      }}
    >
      <div
        style={{
          height: 1,
          background: 'var(--overlay-line-90)',
          flex: 1,
        }}
      />
      <div
        className="text-[10px]"
        style={{
          color: 'var(--accent-research)',
        }}
      >
        end of section
      </div>
      <div
        style={{
          height: 1,
          background: 'var(--overlay-line-90)',
          flex: 1,
        }}
      />
    </div>
  );
};

