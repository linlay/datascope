import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { AuthBoundary } from './Auth';
import './styles.css';
import { PreferencesProvider } from './ui/preferences';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PreferencesProvider><AuthBoundary><App /></AuthBoundary></PreferencesProvider>
  </StrictMode>,
);
