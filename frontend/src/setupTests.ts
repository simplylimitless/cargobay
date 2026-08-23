// jest-dom adds custom jest matchers for asserting on DOM nodes.
// allows you to do things like:
// expect(element).toHaveTextContent(/react/i)
// learn more: https://github.com/testing-library/jest-dom
import '@testing-library/jest-dom';
import { afterEach, vi } from 'vitest';

// Mock localStorage with a real backing store so values actually persist
// across get/set calls within a test, matching browser behavior.
let localStorageStore: Record<string, string> = {};
const mockLocalStorage = {
  getItem: vi.fn((key: string) => localStorageStore[key] ?? null),
  setItem: vi.fn((key: string, value: string) => {
    localStorageStore[key] = String(value);
  }),
  removeItem: vi.fn((key: string) => {
    delete localStorageStore[key];
  }),
  clear: vi.fn(() => {
    localStorageStore = {};
  }),
};

Object.defineProperty(window, 'localStorage', {
  value: mockLocalStorage,
  writable: true,
});

// Mock matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }),
});

// Mock resize observer
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// Mock intersection observer
global.IntersectionObserver = class IntersectionObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// Mock console methods to reduce noise in tests
const originalConsoleError = console.error;
console.error = (...args) => {
  // Only log if it's not a React warning about async rendering
  const message = args.join(' ');
  if (!message.includes('act(') && !message.includes('React state update')) {
    originalConsoleError(...args);
  }
};

// Cleanup after each test
afterEach(() => {
  vi.clearAllMocks();
  localStorageStore = {};
  document.body.innerHTML = '';
});

// Global test timeout
vi.setConfig({ testTimeout: 10000 });
