import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

/** Comes up from the bottom of a phone, stands in the middle of a desktop. A pull down closes it. */
export function Sheet({ title, onClose, children, className = '' }: { title: string; onClose: () => void; children: ReactNode; className?: string }) {
  const box = useRef<HTMLDialogElement>(null);
  const pull = useRef<{ y: number; id: number } | null>(null);
  useEffect(() => {
    const d = box.current!;
    d.showModal();
    const was = document.documentElement.style.overflow;
    document.documentElement.style.overflow = 'hidden';
    return () => { document.documentElement.style.overflow = was; };
  }, []);
  const move = (e: React.PointerEvent) => {
    if (pull.current?.id !== e.pointerId) return;
    box.current!.style.translate = `0 ${Math.max(0, e.clientY - pull.current.y)}px`;
  };
  const drop = (e: React.PointerEvent) => {
    if (pull.current?.id !== e.pointerId) return;
    const by = e.type === 'pointercancel' ? 0 : e.clientY - pull.current.y;
    pull.current = null;
    if (by > 80) onClose();
    else box.current!.style.translate = '';
  };
  return createPortal(
    <dialog ref={box} className={`sheet glass ${className}`} aria-label={title} onCancel={(e) => { e.preventDefault(); onClose(); }} onClick={(e) => { if (e.target === box.current) onClose(); }}>
      <div className="sheet-in">
        <header className="sheet-head" onPointerDown={(e) => { if ((e.target as Element).closest('button')) return; pull.current = { y: e.clientY, id: e.pointerId }; e.currentTarget.setPointerCapture(e.pointerId); }} onPointerMove={move} onPointerUp={drop} onPointerCancel={drop}>
          <h2>{title}</h2><button type="button" className="sheet-x" onClick={onClose} aria-label="关闭"><svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M2 2l12 12M14 2L2 14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" /></svg></button>
        </header>
        <div className="sheet-body">{children}</div>
      </div>
    </dialog>, document.body);
}
