import { useEffect } from 'react';

// This function is also installed inside sandboxed HTML previews. Keep it
// self-contained so preview documents do not need access to the parent page.
export function installAutoHideScrollbars(root: Document = document) {
 const timers = new Map<HTMLElement, ReturnType<typeof setTimeout>>();
 const activate = (element: HTMLElement) => {
  element.dataset.scrollActive = 'true';
  const timer = timers.get(element); if (timer) clearTimeout(timer);
  timers.set(element, setTimeout(() => { delete element.dataset.scrollActive; timers.delete(element); }, 3000));
 };
 const scroll = (event: Event) => {
  const element = event.target === root ? root.scrollingElement : event.target;
  if (element instanceof HTMLElement) activate(element);
 };
 let lastPointer = 0;
 const pointer = (event: Event) => {
  const now = Date.now(); if (now - lastPointer < 100) return; lastPointer = now;
  for (const node of event.composedPath()) {
   if (!(node instanceof HTMLElement)) continue;
   const style = getComputedStyle(node);
   if ((node.scrollHeight > node.clientHeight && /auto|scroll/.test(style.overflowY)) || (node.scrollWidth > node.clientWidth && /auto|scroll/.test(style.overflowX))) { activate(node); break; }
  }
 };
 root.addEventListener('scroll', scroll, { capture: true, passive: true });
 root.addEventListener('pointermove', pointer, { capture: true, passive: true });
 root.addEventListener('pointerdown', pointer, { capture: true, passive: true });
 return () => {
  root.removeEventListener('scroll', scroll, true); root.removeEventListener('pointermove', pointer, true); root.removeEventListener('pointerdown', pointer, true);
  for (const [element, timer] of timers) { clearTimeout(timer); delete element.dataset.scrollActive; }
  timers.clear();
 };
}
export function useAutoHideScrollbars() { useEffect(() => installAutoHideScrollbars(), []); }

export const previewScrollbarStyles = `* { scrollbar-width: thin; scrollbar-color: transparent transparent; }
*::-webkit-scrollbar { width: 5px; height: 5px; }
*::-webkit-scrollbar-track,*::-webkit-scrollbar-corner { background: transparent; border: 0; }
*::-webkit-scrollbar-thumb { background: transparent; border: 0; border-radius: 5px; }
[data-scroll-active="true"] { scrollbar-color: rgba(128,128,128,.5) transparent; }
[data-scroll-active="true"]::-webkit-scrollbar-thumb { background: rgba(128,128,128,.5); }`;
