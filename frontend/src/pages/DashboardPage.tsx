import { useCallback } from 'react';
import {
  Blocks,
  Cpu,
  Coins,
  Users,
  Package,
  TrendingUp,
  Pickaxe,
  Droplets,
  Database,
  ArrowUpDown,
  Shield,
  RefreshCw,
} from 'lucide-react';
import api, { type NodeStatus } from '../services/api';
import { useApi } from '../hooks/useApi';
import { LoadingState, ErrorState } from '../components/States';

export default function DashboardPage() {
  const fetcher = useCallback(() => api.status(), []);
  const { data: status, loading, error, refetch } = useApi<NodeStatus>(fetcher, {
    autoRefreshMs: 8000,
  });

  if (loading && !status) return <LoadingState message="Connecting to node..." />;
  if (error && !status) return <ErrorState message={error} onRetry={refetch} />;
  if (!status) return null;

  const supplyPct = status.max_supply > 0
    ? (((status.total_mined + status.total_faucet) / status.max_supply) * 100).toFixed(2)
    : '0';

  return (
    <div>
      <div className="page-header">
        <h1>Node Dashboard</h1>
        <p>
          Noda v{status.version} &middot; {status.tx_model.replace(/_/g, ' ')}
          <button
            className="btn btn-ghost btn-sm"
            onClick={refetch}
            style={{ marginLeft: 12, verticalAlign: 'middle' }}
          >
            <RefreshCw size={14} />
          </button>
        </p>
      </div>

      {/* Primary Stats */}
      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-label"><Blocks size={14} /> Block Height</div>
          <div className="stat-value primary">{status.block_height.toLocaleString()}</div>
          <div className="stat-sub">{status.chain_length} blocks in chain</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><Coins size={14} /> Total Supply</div>
          <div className="stat-value accent">
            {(status.total_mined + status.total_faucet).toLocaleString()} N
          </div>
          <div className="stat-sub">{supplyPct}% of {status.max_supply.toLocaleString()} max</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><Pickaxe size={14} /> Block Reward</div>
          <div className="stat-value">{status.block_reward} N</div>
          <div className="stat-sub">Total mined: {status.total_mined.toLocaleString()} N</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><Users size={14} /> Peers</div>
          <div className="stat-value">{status.peers + status.p2p_peers}</div>
          <div className="stat-sub">HTTP: {status.http_peers} &middot; P2P: {status.p2p_peers}</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><Package size={14} /> Mempool</div>
          <div className="stat-value">{status.mempool_size}</div>
          <div className="stat-sub">pending transactions</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><Database size={14} /> UTXO Set</div>
          <div className="stat-value">{status.utxo_count.toLocaleString()}</div>
          <div className="stat-sub">unspent outputs</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><TrendingUp size={14} /> Cumulative Work</div>
          <div className="stat-value" style={{ fontSize: '1rem' }}>
            {status.cumulative_work.length > 12
              ? status.cumulative_work.slice(0, 12) + '...'
              : status.cumulative_work}
          </div>
          <div className="stat-sub">proof-of-work difficulty sum</div>
        </div>

        <div className="stat-card">
          <div className="stat-label"><Cpu size={14} /> Mining</div>
          <div className="stat-value">
            {status.mining_enabled ? (
              <span className="badge badge-success">Active</span>
            ) : (
              <span className="badge badge-danger">Inactive</span>
            )}
          </div>
          <div className="stat-sub">
            {status.blocks_mined_by_node > 0
              ? `${status.blocks_mined_by_node} blocks mined by this node`
              : 'No blocks mined locally'}
          </div>
        </div>
      </div>

      {/* Faucet Info */}
      <div className="card section-gap">
        <div className="card-header">
          <div className="card-title">
            <Droplets size={20} /> Faucet Status
          </div>
          {status.faucet_active ? (
            <span className="badge badge-success">Active</span>
          ) : (
            <span className="badge badge-danger">Exhausted</span>
          )}
        </div>

        <div className="stats-grid" style={{ marginBottom: 0 }}>
          <div>
            <div className="stat-label">Distributed</div>
            <div className="stat-value" style={{ fontSize: '1.2rem' }}>
              {status.total_faucet.toLocaleString()} N
            </div>
          </div>
          <div>
            <div className="stat-label">Remaining</div>
            <div className="stat-value" style={{ fontSize: '1.2rem' }}>
              {(status.faucet_remaining ?? 0).toLocaleString()} N
            </div>
          </div>
          <div>
            <div className="stat-label">Faucet Balance</div>
            <div className="stat-value" style={{ fontSize: '1.2rem' }}>
              {(status.faucet_balance ?? 0).toLocaleString()} N
            </div>
          </div>
        </div>

        {status.max_supply > 0 && (
          <div className="progress-bar-track" style={{ marginTop: 16 }}>
            <div
              className="progress-bar-fill"
              style={{
                width: `${Math.min(
                  100,
                  ((status.total_faucet / 1000000) * 100)
                )}%`,
              }}
            />
          </div>
        )}
      </div>

      {/* Security Info */}
      <div className="card section-gap">
        <div className="card-header">
          <div className="card-title">
            <Shield size={20} /> Security
          </div>
        </div>
        <div className="stats-grid" style={{ marginBottom: 0 }}>
          <div>
            <div className="stat-label">Wallet HTTP Mode</div>
            <div className="stat-value" style={{ fontSize: '1rem' }}>
              {status.insecure_wallet_http ? (
                <span className="badge badge-warning">Insecure (Dev)</span>
              ) : (
                <span className="badge badge-success">Secure</span>
              )}
            </div>
          </div>
          <div>
            <div className="stat-label"><ArrowUpDown size={14} /> TX Model</div>
            <div className="stat-value" style={{ fontSize: '1rem' }}>
              {status.tx_model.replace(/_/g, ' ').toUpperCase()}
            </div>
          </div>
          <div>
            <div className="stat-label">Chain Selection</div>
            <div className="stat-value" style={{ fontSize: '1rem' }}>
              Cumulative Work
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
