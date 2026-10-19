const paths = {
  search: '<circle cx="10.8" cy="10.8" r="7.3"/><path d="m16.2 16.2 4.3 4.3"/>',
  play: '<path d="m8 4 12 8-12 8Z"/>',
  pause: '<path d="M7 4h3v16H7zM14 4h3v16h-3z"/>',
  heart: '<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0l-1 1-1-1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z"/>',
  arrow: '<path d="M4 12h15m-6-6 6 6-6 6"/>',
  back: '<path d="M20 12H5m6-6-6 6 6 6"/>',
  chevron: '<path d="m6 9 6 6 6-6"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  volume: '<path d="m11 4-6 5H2v6h3l6 5zM15 8a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14"/>',
  muted: '<path d="m11 4-6 5H2v6h3l6 5zM16 9l6 6m0-6-6 6"/>',
  fullscreen: '<path d="M8 3H3v5m13-5h5v5M3 16v5h5m8 0h5v-5"/>',
  settings: '<path d="m9.5 3-.6 2.4-2 .9-2.2-.7-2.5 4.3 1.7 1.7v2.3l-1.7 1.7 2.5 4.3 2.3-.7 1.9.9.6 2.4h5l.6-2.4 2-.9 2.2.7 2.5-4.3-1.7-1.7v-2.3l1.7-1.7-2.5-4.3-2.3.7-1.9-.9-.6-2.4z"/><circle cx="12" cy="12.7" r="3"/>',
  warning: '<path d="m12 3 10 18H2z"/><path d="M12 9v5m0 3h.01"/>',
  retry: '<path d="M20 7V2l-3 3a9 9 0 1 0 3.5 11M20 2h-5"/>',
  empty: '<circle cx="10" cy="10" r="7"/><path d="m15 15 6 6m-14-14 6 6m0-6-6 6"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
};

export function icon(name, className = "") {
  return `<svg class="icon ${className}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.55" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.play}</svg>`;
}
