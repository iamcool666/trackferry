/** The other browser whose song the last send to a speaker replaced; '' when none. */
let took = '';
export const setTook = (from?: string) => { took = from ?? ''; };
/** Added to a "sent" message so whoever took the speaker knows from whom. */
export const tookNote = () => (took ? `，接替了${took}在放的歌` : '');
