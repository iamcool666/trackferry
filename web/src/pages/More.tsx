import type { Place } from '../nav';

const rows: { page: 'speakers' | 'tv' | 'say' | 'flow' | 'account'; label: string; note: string; icon: string }[] = [
  { page: 'speakers', label: '音箱', note: '接管、音量、压住小爱的方式', icon: 'M7 3.5h10a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-13a2 2 0 0 1 2-2zM12 10.5a3.4 3.4 0 1 1 0 6.8 3.4 3.4 0 0 1 0-6.8zM12 7h.01' },
  { page: 'tv', label: '电视', note: '配对与已连接的电视', icon: 'M3 4h18v13H3zM12 17v4m-4 0h8' },
  { page: 'say', label: '怎么说', note: '能听懂的口令', icon: 'M4 5.5h16v11h-9l-4.5 3.6v-3.6H4zM8.5 11h.01M12 11h.01M15.5 11h.01' },
  { page: 'flow', label: '流转', note: '每一次点歌怎么走的', icon: 'M4 7h7c3 0 3 10 6 10h3M4 17h5M16 7h4M17.5 4.5 20 7l-2.5 2.5M17.5 14.5 20 17l-2.5 2.5' },
  { page: 'account', label: '设置', note: '账号、飞牛音乐、音源、更新', icon: 'M4 7h9M18 7h2M4 17h2M11 17h9M15.5 4.6a2.4 2.4 0 1 1 0 4.8 2.4 2.4 0 0 1 0-4.8zM8.5 14.6a2.4 2.4 0 1 1 0 4.8 2.4 2.4 0 0 1 0-4.8z' },
];

/** What is wanted less often on a phone. */
export function More({ navigate, dot }: { navigate: (p: Place) => void; dot: boolean }) {
  return (
    <div className="more-page">
      <h1>更多</h1>
      <ul>
        {rows.map((r) => (
          <li key={r.page}><a href={`#/${r.page}`} onClick={(e) => { e.preventDefault(); navigate({ page: r.page }); }}>
            <svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={r.icon} /></svg>
            <span><b>{r.label}{r.page === 'account' && dot && <i className="dot" aria-label="需要处理" />}</b><small>{r.note}</small></span>
            <svg viewBox="0 0 8 14" width="8" height="14" aria-hidden="true"><path d="M1 1l6 6-6 6" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
          </a></li>
        ))}
      </ul>
    </div>
  );
}
