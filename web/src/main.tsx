import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { AuthBoundary } from './Auth';
import './styles.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthBoundary><App /></AuthBoundary>
  </StrictMode>,
);
