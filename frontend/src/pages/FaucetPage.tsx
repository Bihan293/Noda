import { useState } from 'react';
import { Droplets, Send, Coins } from 'lucide-react';
import api, { type FaucetResponse } from '../services/api';
import { useWallet, useToast } from '../context/AppContext';
import CopyField from '../components/CopyField';

export default function FaucetPage() {
  const { wallet } = useWallet();
  const { addToast } = useToast();
  const [address, setAddress] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<FaucetResponse | null>(null);

  const handleRequest = async () => {
    const target = address.trim() || wallet?.address;
    if (!target) {
      addToast('Enter an address or generate a wallet first', 'warning');
      return;
    }

    setLoading(true);
    setResult(null);
    try {
      const res = await api.faucet(target);
      setResult(res);
      addToast(`${res.amount} N sent to your address!`, 'success');
    } catch (err: unknown) {
      addToast(err instanceof Error ? err.message : 'Faucet request failed', 'error');
    } finally {
      setLoading(false);
    }
  };

  const useMyAddress = () => {
    if (wallet?.address) {
      setAddress(wallet.address);
      addToast('Wallet address filled', 'info');
    }
  };

  return (
    <div>
      <div className="page-header">
        <h1>Faucet</h1>
        <p>Request free testnet coins (100 N per request)</p>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title">
            <Droplets size={20} /> Request Coins
          </div>
        </div>

        <div className="input-group">
          <label>Recipient Address</label>
          <input
            className="input mono"
            placeholder={wallet?.address || 'Enter hex address...'}
            value={address}
            onChange={(e) => setAddress(e.target.value)}
          />
        </div>

        {wallet && !address && (
          <p
            style={{
              fontSize: '0.78rem',
              color: 'var(--text-secondary)',
              marginTop: -8,
              marginBottom: 12,
            }}
          >
            Leave empty to use your wallet address, or{' '}
            <button
              onClick={useMyAddress}
              style={{
                background: 'none',
                border: 'none',
                color: 'var(--noda-primary-light)',
                cursor: 'pointer',
                padding: 0,
                font: 'inherit',
                textDecoration: 'underline',
              }}
            >
              click here to fill it
            </button>
          </p>
        )}

        <button
          className="btn btn-accent btn-full"
          onClick={handleRequest}
          disabled={loading}
          style={{ marginTop: 8 }}
        >
          {loading ? (
            <>
              <div className="spinner" /> Requesting...
            </>
          ) : (
            <>
              <Send size={18} /> Request 100 N
            </>
          )}
        </button>
      </div>

      {/* Result */}
      {result && (
        <div className="card section-gap">
          <div className="card-header">
            <div className="card-title">
              <Coins size={20} /> Transaction Submitted
            </div>
            <span className="badge badge-success">Pending</span>
          </div>

          <div className="key-field">
            <div className="key-field-label">Amount</div>
            <div className="stat-value accent" style={{ fontSize: '1.5rem' }}>
              {result.amount} N
            </div>
          </div>

          <div className="key-field">
            <div className="key-field-label">Transaction ID</div>
            <CopyField text={result.txid} label="TXID" />
          </div>

          <div className="key-field">
            <div className="key-field-label">Recipient</div>
            <CopyField text={result.to} label="Address" />
          </div>

          <div className="key-field">
            <div className="key-field-label">Status</div>
            <div>
              <span className="badge badge-warning">
                {result.status} &middot; {result.confirmations} confirmations
              </span>
            </div>
          </div>

          {result.faucet_remaining !== undefined && (
            <div className="key-field">
              <div className="key-field-label">Faucet Remaining</div>
              <div style={{ color: 'var(--text-primary)' }}>
                {result.faucet_remaining.toLocaleString()} N
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
