import React from 'react';

interface ActionInteractionProps {
  interaction: any;
  collapsedSections: Record<string, boolean>;
}

export const ActionInteraction: React.FC<ActionInteractionProps> = ({
  interaction,
  collapsedSections,
}) => {
  // Respect collapsed sections
  try {
    const sid = (interaction as any).sectionId as string | undefined;
    if (sid && collapsedSections[sid]) return null;
  } catch {}

  return (
    <div
      key={interaction.id}
      className="my-2 p-3 rounded"
      style={{
        background: 'var(--accent-research-10)',
        color: 'var(--accent-research)',
      }}
    >
      <div className="text-12 font-medium">Executing command:</div>
      <div className="text-12 font-mono mt-1">{interaction.content}</div>
    </div>
  );
};

