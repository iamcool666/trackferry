import { useEffect, useState, type ReactNode } from 'react';

export type Light = 'ok' | 'warn' | 'bad' | 'off';

/** One subject of the settings page: what it is, how it stands, what can be done. */
export function Setting({ id, icon, title, says, light, state, actions, force = false, children }: {
  id: string; icon: ReactNode; title: string; says?: string; light?: Light; state?: ReactNode; actions?: ReactNode; children?: ReactNode;
  /** Something in it needs an answer now. */
  force?: boolean;
}) {
  // A phone shows one line each; a tap opens it. What has gone wrong is open already.
  const need = force || light === 'bad';
  const [open, setOpen] = useState(need);
  useEffect(() => { if (need) setOpen(true); }, [need]);
  return (
    <section className={`st ${open ? 'open' : ''}`} id={`set-${id}`}>
      <header className="st-head">
        <button type="button" className="st-toggle" aria-expanded={open} aria-label={title} onClick={() => setOpen(!open)}><svg viewBox="0 0 12 8" width="12" height="8" aria-hidden="true"><path d="M1 1.5l5 5 5-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg></button>
        <span className="st-icon">{icon}</span>
        <div><h2>{title}{state && <em className={`st-state ${light ?? 'off'}`}>{state}</em>}</h2>{says && <p>{says}</p>}</div>
        {actions && <div className="st-actions">{actions}</div>}
      </header>
      {children && <div className="st-body">{children}</div>}
    </section>
  );
}

const draw = (d: ReactNode) => <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{d}</svg>;
export const LockIcon = () => draw(<><rect x="5" y="10.5" width="14" height="9.5" rx="2" /><path d="M8 10.5V8a4 4 0 0 1 8 0v2.5M12 14.5v2" /></>);
export const WavesIcon = () => draw(<path d="M3 8.5c3-2.4 6-2.4 9 0s6 2.4 9 0M3 15.5c3-2.4 6-2.4 9 0s6 2.4 9 0" />);
export const HouseIcon = () => draw(<path d="M4 11.5 12 4.5l8 7M6 10v9.5h12V10M12 19.5v-5M9 12.5l3 2 3-2" />);
export const NotesIcon = () => draw(<><path d="M9 18V6l10-2v12" /><circle cx="6.5" cy="18" r="2.5" /><circle cx="16.5" cy="16" r="2.5" /></>);
export const PhoneIcon = () => draw(<><rect x="7" y="3" width="10" height="18" rx="2.2" /><path d="M11 17.5h2" /></>);
