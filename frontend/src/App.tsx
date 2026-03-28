import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AppProviders } from './context/AppContext';
import Layout from './components/Layout';
import DashboardPage from './pages/DashboardPage';
import WalletPage from './pages/WalletPage';
import FaucetPage from './pages/FaucetPage';
import ExplorerPage from './pages/ExplorerPage';
import MempoolPage from './pages/MempoolPage';
import PeersPage from './pages/PeersPage';

export default function App() {
  return (
    <AppProviders>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/wallet" element={<WalletPage />} />
            <Route path="/faucet" element={<FaucetPage />} />
            <Route path="/explorer" element={<ExplorerPage />} />
            <Route path="/mempool" element={<MempoolPage />} />
            <Route path="/peers" element={<PeersPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </AppProviders>
  );
}
