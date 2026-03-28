import { useCallback } from 'react';
import { Globe, RefreshCw, Radio } from 'lucide-react';
import api, { type PeersResponse } from '../services/api';
import { useApi } from '../hooks/useApi';
import { LoadingState, ErrorState, EmptyState } from '../components/States';

export default function PeersPage() {
  const fetcher = useCallback(() => api.peers(), []);
  const { data: peersData, loading, error, refetch } = useApi<PeersResponse>(fetcher, {
    autoRefreshMs: 10000,
  });

  if (loading && !peersData) return <LoadingState message="Loading peers..." />;
  if (error && !peersData) return <ErrorState message={error} onRetry={refetch} />;
  if (!peersData) return null;

  const peers = peersData.peers || [];

  return (
    <div>
      <div className="page-header">
        <h1>Network Peers</h1>
        <p>
          {peers.length} connected peer{peers.length !== 1 ? 's' : ''}
          <button
            className="btn btn-ghost btn-sm"
            onClick={refetch}
            style={{ marginLeft: 12, verticalAlign: 'middle' }}
          >
            <RefreshCw size={14} />
          </button>
        </p>
      </div>

      {peers.length === 0 ? (
        <div className="card">
          <EmptyState icon={Radio} message="No peers connected — this node is running standalone" />
        </div>
      ) : (
        <div className="card">
          <div className="card-header">
            <div className="card-title">
              <Globe size={20} /> Connected Peers
            </div>
            <span className="badge badge-success">{peers.length} online</span>
          </div>

          <div className="peer-list">
            {peers.map((peer, i) => (
              <div key={i} className="peer-item">
                <div className="peer-dot" />
                <span className="mono" style={{ flex: 1 }}>{peer}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
