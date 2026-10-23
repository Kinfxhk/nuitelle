// Tiny public-domain catalog example. No network, no filesystem.
var FILMS = [
  { id: "nosferatu-1922", title: "Nosferatu", year: 1922, overview: "Murnau, public domain." },
  { id: "metropolis-1927", title: "Metropolis", year: 1927, overview: "Lang, public domain in many regions." },
  { id: "night-living-dead-1968", title: "Night of the Living Dead", year: 1968, overview: "Romero, public domain." },
  { id: "his-girl-friday-1940", title: "His Girl Friday", year: 1940, overview: "Hawks, public domain." }
];
function catalogSearch(input) {
  var q = (input.query || "").toLowerCase();
  return { items: FILMS.filter(function (f) { return f.title.toLowerCase().indexOf(q) >= 0; }) };
}
