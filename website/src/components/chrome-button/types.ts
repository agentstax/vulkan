// primary: the action the surface exists for -- Run, Produce, auto-run
// quiet: a secondary action sharing the same chrome at lower weight
// close: the era's square close box at the right end of a title bar, whose
//        label is a glyph, so the caller's ariaLabel is its only real name
export type ChromeButtonTone = 'primary' | 'quiet' | 'close';
