// Runs in a bare JS VM: no filesystem, no network, no timers.
// Return tags (or an empty array) for the scanned file.
function scanClassify(input) {
  var tags = [];
  if (/sample|demo/i.test(input.title)) tags.push("sample");
  if (/4k|2160/i.test(input.title)) tags.push("4k");
  return { tags: tags };
}
