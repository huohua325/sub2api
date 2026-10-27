// Apply before styles load. Match useTheme: saved dark preference, otherwise light.
try {
  var mileDark = localStorage.getItem('theme') === 'dark';
  document.documentElement.classList.toggle('dark', mileDark);
  document.documentElement.style.colorScheme = mileDark ? 'dark' : 'light';
} catch (_) { /* Light is the default when storage is unavailable. */ }
