// A wrapper the built-in patterns do not know: without .weave/patterns.json the
// route below is invisible, which is exactly the gap the patterns file fills.
export const legacy = defineRoute('/api/legacy');
