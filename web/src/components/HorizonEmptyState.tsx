import React from 'react';

interface HorizonEmptyStateProps {
  onCreateProject: () => void;
  existingProjects: Array<{ id: string; displayName: string }>;
  onSelectProject: (projectId: string) => void;
}

export const HorizonEmptyState: React.FC<HorizonEmptyStateProps> = ({
  onCreateProject,
  existingProjects,
  onSelectProject,
}) => {
  const hasExistingProjects = existingProjects.length > 0;

  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '32px 24px',
      }}
    >
      <div
        style={{
          width: '100%',
          maxWidth: 620,
          textAlign: 'left',
        }}
      >
        <div
          style={{
            fontSize: '11px',
            fontWeight: 500,
            letterSpacing: '0.14em',
            textTransform: 'uppercase',
            color: 'var(--text-tertiary)',
            marginBottom: 10,
          }}
        >
          Horizon
        </div>
        <div
          style={{
            fontSize: 'clamp(32px, 5vw, 46px)',
            fontWeight: 560,
            color: 'var(--text-heading)',
            lineHeight: 1.04,
            maxWidth: 540,
            marginBottom: 16,
          }}
        >
          {hasExistingProjects ? 'Choose or create a monitoring project' : 'Create a monitoring project'}
        </div>
        <div
          style={{
            fontSize: 15,
            color: 'var(--text-secondary-strong)',
            lineHeight: 1.7,
            maxWidth: 560,
            marginBottom: 22,
          }}
        >
          Projects group your lenses into a coherent monitoring structure. Lenses turn the outside world into a focused
          view of dependencies, exposure, and change.
        </div>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 0,
            width: '100%',
            maxWidth: 560,
            border: '1px solid var(--overlay-line-90)',
            borderRadius: 16,
            overflow: 'hidden',
            background: 'var(--bg-elevated)',
            marginBottom: 28,
          }}
        >
          {[
            { label: 'Project', detail: 'Organizes the topic' },
            { label: 'Lenses', detail: 'Define the view' },
            { label: 'Monitoring view', detail: 'Dependencies, exposure, change' },
          ].map((item, index) => (
            <React.Fragment key={item.label}>
              <div
                style={{
                  flex: 1,
                  minWidth: 0,
                  padding: '14px 16px',
                  background: 'transparent',
                }}
              >
                <div
                  style={{
                    color: 'var(--text-heading)',
                    fontSize: 13,
                    fontWeight: 560,
                    marginBottom: 4,
                  }}
                >
                  {item.label}
                </div>
                <div
                  style={{
                    color: 'var(--text-tertiary)',
                    fontSize: 12,
                    lineHeight: 1.45,
                  }}
                >
                  {item.detail}
                </div>
              </div>
              {index < 2 ? (
                <div
                  aria-hidden="true"
                  style={{
                    alignSelf: 'stretch',
                    width: 1,
                    background: 'var(--overlay-line-90)',
                  }}
                />
              ) : null}
            </React.Fragment>
          ))}
        </div>
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 12,
          }}
        >
          <button
            type="button"
            onClick={onCreateProject}
            className="rounded-lg"
            style={{
              padding: '12px 18px',
              fontSize: 14,
              fontWeight: 600,
              backgroundColor: 'var(--bg-elevated)',
              color: 'var(--text-heading)',
              border: '1px solid var(--overlay-line-90)',
              cursor: 'pointer',
              transition: 'background-color 0.2s, transform 0.2s',
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-04)';
              (e.currentTarget as HTMLButtonElement).style.transform = 'translateY(-1px)';
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--bg-elevated)';
              (e.currentTarget as HTMLButtonElement).style.transform = 'translateY(0)';
            }}
          >
            Create project
          </button>
          {hasExistingProjects ? (
            <>
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--text-tertiary)',
                }}
              >
                or choose existing
              </div>
              <select
                defaultValue=""
                onChange={(e) => {
                  const projectId = String(e.target.value || '').trim();
                  if (projectId) onSelectProject(projectId);
                }}
                style={{
                  minWidth: 260,
                  padding: '12px 14px',
                  borderRadius: 10,
                  border: '1px solid var(--overlay-line-90)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-heading)',
                  fontSize: 14,
                  outline: 'none',
                }}
              >
                <option value="" disabled>
                  Select project
                </option>
                {existingProjects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.displayName}
                  </option>
                ))}
              </select>
            </>
          ) : null}
        </div>
        <div
          style={{
            marginTop: 12,
            fontSize: 12,
            color: 'var(--text-tertiary)',
            lineHeight: 1.5,
          }}
        >
          {hasExistingProjects ? null : 'Start with a project. You can define lenses inside it once the workspace is created.'}
        </div>
      </div>
    </div>
  );
};
