import { useEffect, useState } from 'react';
import { isAuthenticated, clearAuth, getRole, isSuperadmin } from './utils/auth';
import { disconnectSSE } from './utils/sseClient';
import { useTanks } from './hooks/useTanks';
import ReportsTab from './components/ReportsTab';
import ChartsTab from './components/ChartsTab';
import TankConfigTab from './components/TankConfigTab';
import PumpControlTab from './components/PumpControlTab';
import LiveMonitorCard from './components/LiveMonitorCard';
import EnvironmentTab from './components/EnvironmentTab';
import LoginPage from './components/LoginPage';
import './App.css';

export default function App() {
    const [loggedIn, setLoggedIn] = useState(() => isAuthenticated());
    if (!loggedIn) {
    return <LoginPage onLogin={() => setLoggedIn(true)} />;
  } else {
    return <MainApp />;
  }
}

function MainApp() {

  const [theme, setTheme] = useState(() => {
    const saved = localStorage.getItem('theme') || 'dark';
    document.documentElement.setAttribute('data-theme', saved);
    return saved;
  });
  const [activeTab, setActiveTab] = useState('monitor');
  const [tank, setTank] = useState(null);
  const role = getRole();
  const canControl = isSuperadmin();
  const { tanks, tankMap, loading: tanksLoading, error: tanksError, refresh: refreshTanks } = useTanks();

  useEffect(() => {
    if (tanksLoading) return;
    if (!tanks.length) {
      setTank(null);
      return;
    }
    if (!tank || !tankMap[tank]) {
      setTank(tanks[0].id);
    }
  }, [tank, tankMap, tanks, tanksLoading]);

  const selectedTankConfig = tank ? tankMap[tank] : null;
  const hasTanks = tanks.length > 0;
  const maxOfAllTanks = hasTanks ? Math.max(...tanks.map((t) => t.maxDistance)) : 1;
  const requiresConfig =
    activeTab !== 'monitor' &&
    activeTab !== 'tanks' &&
    activeTab !== 'pump' &&
    activeTab !== 'environment';

  function handleLogout() {
    disconnectSSE();
    clearAuth();
    setLoggedIn(false);
  }

  function toggleTheme() {
    const next = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
    setTheme(next);
  }

  return (
    <div className="app">
      <header className="app-header">
        <h1>💧 Water Tank Monitor</h1>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {role ? (
            <span className={`role-badge ${canControl ? 'is-admin' : 'is-viewer'}`}>
              {canControl ? 'Superadmin' : 'Viewer · read-only'}
            </span>
          ) : null}
          <button type="button" className="theme-btn" onClick={toggleTheme}>
            {theme === 'dark' ? '☀ Light' : '☾ Dark'}
          </button>
          <button type="button" className="logout-btn" onClick={handleLogout}>
            Logout
          </button>
        </div>
      </header>

      <main
        className={`app-main ${
          activeTab === 'reports'
            ? 'reports-mode'
            : activeTab === 'charts'
            ? 'charts-mode'
            : activeTab === 'tanks'
            ? 'config-mode'
            : activeTab === 'pump'
            ? 'pump-mode'
            : activeTab === 'environment'
            ? 'environment-mode'
            : 'monitor-mode'
        }`}
      >
        <div className="tab-bar">
          <div className="tab-buttons">
            <button
              type="button"
              className={`tab-button ${activeTab === 'monitor' ? 'is-active' : ''}`}
              onClick={() => setActiveTab('monitor')}
            >
              Live Monitor
            </button>
            <button
              type="button"
              className={`tab-button ${activeTab === 'reports' ? 'is-active' : ''}`}
              onClick={() => setActiveTab('reports')}
            >
              Reports
            </button>
            <button
              type="button"
              className={`tab-button ${activeTab === 'charts' ? 'is-active' : ''}`}
              onClick={() => setActiveTab('charts')}
            >
              Charts
            </button>
            <button
              type="button"
              className={`tab-button ${activeTab === 'environment' ? 'is-active' : ''}`}
              onClick={() => setActiveTab('environment')}
            >
              Environment
            </button>
            <button
              type="button"
              className={`tab-button ${activeTab === 'tanks' ? 'is-active' : ''}`}
              onClick={() => setActiveTab('tanks')}
            >
              Tank Config
            </button>
            <button
              type="button"
              className={`tab-button ${activeTab === 'pump' ? 'is-active' : ''}`}
              onClick={() => setActiveTab('pump')}
            >
              Pump
            </button>
          </div>

          <div className={`tank-selector ${activeTab === 'environment' ? 'is-hidden' : ''}`}>
            <label htmlFor="tank-select">Tank</label>
            <select
              id="tank-select"
              value={tank ?? ''}
              onChange={(event) => setTank(event.target.value)}
              disabled={tanksLoading || !tanks.length}
            >
              {tanks.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.name || option.id}
                </option>
              ))}
            </select>
            {tanksError && <span className="tank-error">{tanksError}</span>}
          </div>
        </div>

        {tanksLoading && !hasTanks ? (
          <div className="loading">
            <div className="spinner" />
            <p>Loading tank configuration…</p>
          </div>
        ) : !hasTanks ? (
          <div className="error-card">
            <p>⚠️ No tank configuration found</p>
            <p className="error-hint">Ensure the /tanks endpoint returns at least one entry.</p>
          </div>
        ) : requiresConfig && !selectedTankConfig ? (
          <div className="error-card">
            <p>⚠️ No tank configuration found</p>
            <p className="error-hint">Check the backend response from /tanks and refresh.</p>
          </div>
        ) : activeTab === 'monitor' ? (
          <div className="monitor-grid">
            {tanks.map((cfg) => (
              <LiveMonitorCard
                key={cfg.id}
                config={cfg}
                isSelected={cfg.id === tank}
                onSelect={() => setTank(cfg.id)}
                maxOfAllTanks={maxOfAllTanks}
              />
            ))}
          </div>
        ) : activeTab === 'reports' ? (
          <ReportsTab tank={tank} tankConfig={selectedTankConfig} />
        ) : activeTab === 'charts' ? (
          <ChartsTab tank={tank} tankConfig={selectedTankConfig} />
        ) : activeTab === 'pump' ? (
          <PumpControlTab tankMap={tankMap} canControl={canControl} />
        ) : activeTab === 'environment' ? (
          <EnvironmentTab />
        ) : (
          <TankConfigTab
            tanks={tanks}
            loading={tanksLoading}
            error={tanksError}
            onRefresh={refreshTanks}
          />
        )}
      </main>

      <footer className="app-footer">
        <span>Arduino IoT • Water and Environment Monitoring</span>
      </footer>
    </div>
  );
}
