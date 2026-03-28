import { NavLink, Outlet } from 'react-router-dom';
import {
  LayoutDashboard,
  Wallet,
  Droplets,
  Blocks,
  Package,
  Globe,
} from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import api from '../services/api';
import ToastContainer from './ToastContainer';

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/wallet', icon: Wallet, label: 'Wallet' },
  { to: '/faucet', icon: Droplets, label: 'Faucet' },
  { to: '/explorer', icon: Blocks, label: 'Explorer' },
  { to: '/mempool', icon: Package, label: 'Mempool' },
  { to: '/peers', icon: Globe, label: 'Peers' },
];

export default function Layout() {
  const [online, setOnline] = useState(false);

  const checkHealth = useCallback(async () => {
    try {
      await api.health();
      setOnline(true);
    } catch {
      setOnline(false);
    }
  }, []);

  useEffect(() => {
    checkHealth();
    const interval = setInterval(checkHealth, 15000);
    return () => clearInterval(interval);
  }, [checkHealth]);

  return (
    <div className="app-layout">
      {/* Top navbar */}
      <nav className="navbar">
        <div className="navbar-inner">
          <NavLink to="/" className="navbar-brand">
            <div className="brand-icon">N</div>
            <span>Noda</span>
            <div className="navbar-status">
              <div className={`status-dot ${online ? '' : 'offline'}`} />
              <span className="hide-mobile">{online ? 'Online' : 'Offline'}</span>
            </div>
          </NavLink>

          {/* Desktop navigation */}
          <div className="desktop-nav">
            {navItems.map(({ to, icon: Icon, label }) => (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) => (isActive ? 'active' : '')}
              >
                <Icon size={18} />
                {label}
              </NavLink>
            ))}
          </div>
        </div>
      </nav>

      {/* Main content */}
      <main className="page-container">
        <Outlet />
      </main>

      {/* Mobile bottom navigation */}
      <div className="bottom-nav">
        {navItems.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) => (isActive ? 'active' : '')}
          >
            <Icon />
            <span>{label}</span>
          </NavLink>
        ))}
      </div>

      <ToastContainer />
    </div>
  );
}
