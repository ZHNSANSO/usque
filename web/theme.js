// theme.js
function setTheme(theme) {
    if (theme === 'dark') {
        document.documentElement.classList.add('dark');
        localStorage.setItem('theme', 'dark');
    } else {
        document.documentElement.classList.remove('dark');
        localStorage.setItem('theme', 'light');
    }
}

function toggleTheme() {
    const currentTheme = localStorage.getItem('theme') || 'light';
    if (currentTheme === 'light') {
        setTheme('dark');
    } else {
        setTheme('light');
    }
}

// Apply theme on initial load
const savedTheme = localStorage.getItem('theme');
if (savedTheme) {
    setTheme(savedTheme);
} else {
    // Or use system preference
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    if (prefersDark) {
        setTheme('dark');
    }
}
