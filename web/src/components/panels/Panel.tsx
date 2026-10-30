import type { ReactNode } from "react";

/**
 * Panel is the frame of a panel in the main column: a titled section that
 * is also the scope of its walkthrough (data-walk-scope). id must be unique
 * on the page; the heading gets the id `${id}-title`.
 */
export function Panel({
  id,
  title,
  sub,
  actions,
  className,
  children,
}: {
  id: string;
  title: ReactNode;
  /** One line under the title. */
  sub?: ReactNode;
  /** Controls at the right of the heading, such as a view switch. */
  actions?: ReactNode;
  /** More classes, such as "chart". */
  className?: string;
  children: ReactNode;
}) {
  const heading = `${id}-title`;
  return (
    <section
      className={className === undefined ? "panel" : `panel ${className}`}
      aria-labelledby={heading}
      data-walk-scope
      data-panel={id}
    >
      <div className="panel-head">
        <div>
          <h2 id={heading}>{title}</h2>
          {sub !== undefined && <p>{sub}</p>}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}
