import { useState, useCallback } from 'react';
import {
  Wallet,
  KeyRound,
  Plus,
  Trash2,
  AlertTriangle,
  Eye,
  EyeOff,
  Search,
  Coins,
} from 'lucide-react';
import api, { type KeyPair, type BalanceResponse } from '../services/api';
import { useWallet, useToast } from '../context/AppContext';
import CopyField from '../components/CopyField';
import { LoadingState } from '../components/States';

export default function WalletPage() {
  const { wallet, setWallet, clearWallet } = useWallet();
  const { addToast } = useToast();
  const [generating, setGenerating] = useState(false);
  const [showPrivateKey, setShowPrivateKey] = useState(false);

  // Balance lookup
  const [balanceAddress, setBalanceAddress] = useState('');
  const [balanceResult, setBalanceResult] = useState<BalanceResponse | null>(null);
  const [balanceLoading, setBalanceLoading] = useState(false);

  const handleGenerate = async () => {
    setGenerating(true);
    try {
      const kp: KeyPair = await api.generateKeys();
      setWallet(kp);
      setShowPrivateKey(false);
      addToast('New wallet generated & saved!', 'success');
    } catch (err: unknown) {
      addToast(err instanceof Error ? err.message : 'Failed to generate keys', 'error');
    } finally {
      setGenerating(false);
    }
  };

  const handleClear = () => {
    if (window.confirm('Are you sure? Your private key will be lost if not backed up.')) {
      clearWallet();
      setShowPrivateKey(false);
      addToast('Wallet removed from browser', 'warning');
    }
  };

  const handleCheckBalance = useCallback(async () => {
    const addr = balanceAddress.trim() || wallet?.address;
    if (!addr) {
      addToast('Enter an address to check', 'warning');
      return;
    }
    setBalanceLoading(true);
    try {
      const result = await api.balance(addr);
      setBalanceResult(result);
    } catch (err: unknown) {
      addToast(err instanceof Error ? err.message : 'Failed to fetch balance', 'error');
      setBalanceResult(null);
    } finally {
      setBalanceLoading(false);
    }
  }, [balanceAddress, wallet, addToast]);

  return (
    <div>
      <div className="page-header">
        <h1>Wallet</h1>
        <p>Generate keys, manage your address, check balances</p>
      </div>

      {/* Wallet Card */}
      <div className="card">
        <div className="card-header">
          <div className="card-title">
            <Wallet size={20} /> My Wallet
          </div>
          {wallet ? (
            <button className="btn btn-ghost btn-sm" onClick={handleClear}>
              <Trash2 size={14} /> Remove
            </button>
          ) : null}
        </div>

        {!wallet ? (
          <div style={{ textAlign: 'center', padding: '24px 0' }}>
            <p style={{ color: 'var(--text-secondary)', marginBottom: 20 }}>
              No wallet found. Generate a new key pair to get started.
            </p>
            <button
              className="btn btn-primary"
              onClick={handleGenerate}
              disabled={generating}
            >
              {generating ? (
                <>
                  <div className="spinner" /> Generating...
                </>
              ) : (
                <>
                  <Plus size={18} /> Generate New Wallet
                </>
              )}
            </button>
          </div>
        ) : (
          <div>
            {/* Address */}
            <div className="key-field">
              <div className="key-field-label">
                <KeyRound size={12} style={{ display: 'inline', verticalAlign: 'middle' }} />{' '}
                Address (Public Key)
              </div>
              <CopyField text={wallet.address} label="Address" />
            </div>

            {/* Private Key */}
            <div className="key-field">
              <div className="key-field-label" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <KeyRound size={12} /> Private Key
                <button
                  className="btn btn-ghost btn-sm"
                  onClick={() => setShowPrivateKey(!showPrivateKey)}
                  style={{ marginLeft: 'auto', padding: '4px 10px', minHeight: 'auto' }}
                >
                  {showPrivateKey ? <EyeOff size={14} /> : <Eye size={14} />}
                  {showPrivateKey ? ' Hide' : ' Reveal'}
                </button>
              </div>

              <div className="warning-box">
                <AlertTriangle size={16} />
                <span>
                  <strong>Never share your private key.</strong> Anyone with this key can spend
                  your funds. It is stored in your browser's localStorage — clear your data to
                  remove it.
                </span>
              </div>

              <CopyField
                text={wallet.private_key}
                label="Private Key"
                blurred={!showPrivateKey}
              />
            </div>

            {/* Generate new */}
            <div style={{ marginTop: 16 }}>
              <button
                className="btn btn-ghost"
                onClick={handleGenerate}
                disabled={generating}
              >
                {generating ? <div className="spinner" /> : <Plus size={16} />}
                Generate New Wallet
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Balance Checker */}
      <div className="card section-gap">
        <div className="card-header">
          <div className="card-title">
            <Coins size={20} /> Check Balance
          </div>
        </div>

        <div className="input-group">
          <label>Address</label>
          <div className="input-with-btn">
            <input
              className="input mono"
              placeholder={wallet?.address || 'Enter hex address...'}
              value={balanceAddress}
              onChange={(e) => setBalanceAddress(e.target.value)}
            />
            <button
              className="btn btn-primary"
              onClick={handleCheckBalance}
              disabled={balanceLoading}
            >
              {balanceLoading ? <div className="spinner" /> : <Search size={18} />}
            </button>
          </div>
        </div>

        {wallet && !balanceAddress && (
          <p style={{ fontSize: '0.78rem', color: 'var(--text-secondary)', marginTop: -8 }}>
            Leave empty to check your wallet balance
          </p>
        )}

        {balanceLoading && <LoadingState message="Fetching balance..." size="sm" />}

        {balanceResult && !balanceLoading && (
          <div
            className="stat-card"
            style={{ marginTop: 16, textAlign: 'center' }}
          >
            <div className="stat-label" style={{ justifyContent: 'center' }}>Balance</div>
            <div className="stat-value accent" style={{ fontSize: '2rem' }}>
              {balanceResult.balance.toLocaleString()} N
            </div>
            <div className="stat-sub">
              {balanceResult.utxo_count} UTXO{balanceResult.utxo_count !== 1 ? 's' : ''} &middot;{' '}
              <span className="mono" style={{ fontSize: '0.7rem' }}>
                {balanceResult.address.slice(0, 12)}...{balanceResult.address.slice(-8)}
              </span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
