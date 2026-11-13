// Keeps only the newest answer when requests overlap.
//
// Without it, an older, slower request (e.g. the unfiltered list on page load)
// can finish after a newer one (the search the user just typed) and overwrite
// its result. Usage:
//
//   const fresh = newest();
//   const load = async () => {
//     const current = fresh();
//     const data = await api.get(…);
//     if (!current()) return; // a newer request has started meanwhile
//     render(data);
//   };
export function newest(): () => () => boolean {
  let seq = 0;
  return () => {
    const my = ++seq;
    return () => my === seq;
  };
}
