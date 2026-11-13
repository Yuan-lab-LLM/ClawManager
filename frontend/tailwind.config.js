// Fluent Web ramps for existing utility consumers; runtime logos keep their assets.
const brand = { 50: '#ebf3fc', 100: '#cfe4fa', 200: '#b4d6fa', 300: '#96c6fa', 400: '#479ef5', 500: '#2886de', 600: '#0f6cbd', 700: '#115ea3', 800: '#0f548c', 900: '#0c3b5e', 950: '#061724' };
const neutral = { 50: '#fafafa', 100: '#f5f5f5', 200: '#e0e0e0', 300: '#d1d1d1', 400: '#707070', 500: '#616161', 600: '#424242', 700: '#424242', 800: '#242424', 900: '#242424', 950: '#141414' };

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: { blue: brand, indigo: brand, violet: brand, purple: brand, slate: neutral, gray: neutral },
      fontFamily: { sans: ['Segoe UI', '-apple-system', 'BlinkMacSystemFont', 'PingFang SC', 'Microsoft YaHei', 'sans-serif'] },
      borderRadius: { DEFAULT: '4px', md: '4px', lg: '8px', xl: '8px', '2xl': '8px', '3xl': '12px' },
      boxShadow: {
        sm: 'var(--cm-shadow-2)', DEFAULT: 'var(--cm-shadow-4)', md: 'var(--cm-shadow-8)',
        lg: 'var(--cm-shadow-16)', xl: 'var(--cm-shadow-28)', '2xl': 'var(--cm-shadow-64)',
      },
    },
  },
  plugins: [],
}
