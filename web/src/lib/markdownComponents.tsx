import React from 'react';
import ReactMarkdown, { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';

/**
 * Общие компоненты для рендеринга markdown в сообщениях
 */
export const markdownComponents: Components = {
  p: (
    props: React.DetailedHTMLProps<
      React.HTMLAttributes<HTMLParagraphElement>,
      HTMLParagraphElement
    >,
  ) => <div {...props} className="md-p" />,
  a: (
    props: React.DetailedHTMLProps<
      React.AnchorHTMLAttributes<HTMLAnchorElement>,
      HTMLAnchorElement
    >,
  ) => (
    <a {...props} target="_blank" rel="noreferrer" className="underline" />
  ),
  code: (
    props: {
      inline?: boolean;
      className?: string;
      children?: React.ReactNode;
    } & React.DetailedHTMLProps<
      React.HTMLAttributes<HTMLElement>,
      HTMLElement
    >,
  ) => {
    const { inline, className, children, ...rest } = props;
    const text =
      typeof children === 'string'
        ? children
        : Array.isArray(children)
          ? children.join('')
          : String(children ?? '');
    const isSingleLineBlock =
      !inline &&
      !/\n/.test(text) &&
      text.trim().length > 0 &&
      text.trim().length <= 80;
    if (inline || isSingleLineBlock) {
      return (
        <code
          {...rest}
          style={{
            backgroundColor: 'var(--white-10)',
            padding: '0 3px',
            borderRadius: 4,
          }}
        >
          {text}
        </code>
      );
    }
    return <code {...rest} className={className}>{children}</code>;
  },
  ul: (
    props: React.DetailedHTMLProps<
      React.HTMLAttributes<HTMLUListElement>,
      HTMLUListElement
    >,
  ) => <ul {...props} style={{ paddingLeft: '1.25rem' }} />,
  ol: (
    props: React.DetailedHTMLProps<
      React.OlHTMLAttributes<HTMLOListElement>,
      HTMLOListElement
    >,
  ) => <ol {...props} style={{ paddingLeft: '1.25rem' }} />,
};

/**
 * Компонент для рендеринга markdown с общими настройками
 */
export const MarkdownRenderer: React.FC<{
  content: string;
  className?: string;
  style?: React.CSSProperties;
  useRaw?: boolean;
  skipWrapper?: boolean;
}> = ({ content, className = 'text-13 markdown', style, useRaw = false, skipWrapper = false }) => {
  const markdownContent = (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      rehypePlugins={useRaw ? [rehypeRaw] : []}
      components={markdownComponents}
    >
      {content}
    </ReactMarkdown>
  );

  if (skipWrapper) {
    return markdownContent;
  }

  return (
    <div className={className} style={style}>
      {markdownContent}
    </div>
  );
};

