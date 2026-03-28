import { useCallback } from 'react';
import { Package, ArrowRight, RefreshCw, Inbox } from 'lucide-react';
import api, { type MempoolResponse, type Transaction } from '../services/api';
import { useApi } from '../hooks/useApi';
import { LoadingState, ErrorState, EmptyState } from '../components/States';

function shortHash(h: string) {
  if (!h || h.length < 16) return h || '—';
  return h.slice(0, 8) + '...' + h.slice(-8);
}

function MempoolTx({ tx }: { tx: Transaction }) {
  return (
    <div className="tx-item" style={{ margin: 0 }}>
      <div className="tx-id mono" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span className="badge badge-warning">Pending</span>
        {tx.id}
      </div>
      <div className="tx-io" style={{ marginTop: 8 }}>
        <div className="tx-io-list">
          {tx.inputs?.map((inp, i) => (
            <div key={i} className="tx-io-item">
              <span className="tx-address mono" title={inp.public_key}>
                {shortHash(inp.public_key)}
              </span>
              <span className="mono" style={{ fontSize: '0.72rem', color: 'var(--text-secondary)' }}>
                #{inp.vout}
              </span>
            </div>
          )) || (
            <div className="tx-io-item">
              <span style={{ color: 'var(--noda-primary-light)', fontSize: '0.78rem' }}>
                Faucet / Coinbase
              </span>
            </div>
          )}
        </div>
        <div className="tx-arrow">
          <ArrowRight size={16} />
        </div>
        <div className="tx-io-list">
          {tx.outputs?.map((out, i) => (
            <div key={i} className="tx-io-item">
              <span className="tx-address mono" title={out.address}>
                {shortHash(out.address)}
              </span>
              <span className="tx-amount">{out.amount} N</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export default function MempoolPage() {
  const fetcher = useCallback(() => api.mempool(), []);
  const { data: mempool, loading, error, refetch } = useApi<MempoolResponse>(fetcher, {
    autoRefreshMs: 5000,
  });

  if (loading && !mempool) return <LoadingState message="Loading mempool..." />;
  if (error && !mempool) return <ErrorState message={error} onRetry={refetch} />;
  if (!mempool) return null;

  return (
    <div>
      <div className="page-header">
        <h1>Mempool</h1>
        <p>
          {mempool.size} pending transaction{mempool.size !== 1 ? 's' : ''}
          <button
            className="btn btn-ghost btn-sm"
            onClick={refetch}
            style={{ marginLeft: 12, verticalAlign: 'middle' }}
          >
            <RefreshCw size={14} />
          </button>
        </p>
      </div>

      {mempool.size === 0 || !mempool.transactions || mempool.transactions.length === 0 ? (
        <div className="card">
          <EmptyState icon={Inbox} message="Mempool is empty — no pending transactions" />
        </div>
      ) : (
        <div className="card">
          <div className="card-header">
            <div className="card-title">
              <Package size={20} /> Pending Transactions
            </div>
            <span className="badge badge-info">{mempool.size}</span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            {mempool.transactions.map((tx, i) => (
              <MempoolTx key={tx.id || i} tx={tx} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
