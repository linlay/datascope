import { lazy, StrictMode, Suspense, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { AuthBoundary } from './Auth';
import './styles.css';
import { PreferencesProvider } from './ui/preferences';

const App = lazy(() => import('./App').then(module => ({ default: module.App })));

const BusinessLogin = lazy(() => import('./pages/BusinessLogin'));

function Entry() {
 const [hash, setHash] = useState(window.location.hash);
 useEffect(() => {
  const changed = () => setHash(window.location.hash);
  window.addEventListener('hashchange', changed);
  window.addEventListener('popstate', changed);
  return () => { window.removeEventListener('hashchange', changed); window.removeEventListener('popstate', changed); };
 }, []);
 const route = hash.split('?')[0];
 if (route === '#/business-login') {
  const database = new URLSearchParams(hash.split('?')[1] || '').get('database') || '';
  return <Suspense fallback={null}><BusinessLogin key={database} database={database} /></Suspense>;
 }
 return <AuthBoundary><Suspense fallback={null}><App /></Suspense></AuthBoundary>;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PreferencesProvider><Entry /></PreferencesProvider>
  </StrictMode>,
);
