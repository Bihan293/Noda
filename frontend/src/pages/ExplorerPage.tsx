import { useState, useCallback } from 'react';
import {
  Blocks,
  ChevronDown,
  ArrowRight,
  Clock,
  Hash,
  Pickaxe,
  RefreshCw,
  Layers,
} from 'lucide-react';
import api, { type Chain, type Block, type Transaction } from '../services/api';
import { useApi } from '../hooks/useApi';
import { LoadingState, ErrorState, EmptyState } from '../components/States';

function formatTime(ts: string) {
  try {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    return d.toLocaleString();
  } catch {
    return ts;
  }
}

function shortHash(h: string) {
  if (!h || h.length < 16) return h || '—';
  return h.slice(0, 8) + '...' + h.slice(-8);
}

function TxItem({ tx }: { tx: Transaction }) {
  const isCoinbase = !tx.inputs || tx.inputs.length === 0;

  return (
    <div className="tx-item">
      <div className="tx-id mono">
        {isCoinbase && <span className="tx-coinbase-badge"><Pickaxe size={10} /> Coinbase</span>}{' '}
        {tx.id}
      </div>
      <div className="tx-io">
        <div className="tx-io-list">
          {isCoinbase ? (
            <div className="tx-io-item">
              <span className="tx-address" style={{ color: 'var(--noda-primary-light)' }}>
                Block Reward
              </span>
            </div>
          ) : (
            tx.inputs?.map((inp, i) => (
              <div key={i} className="tx-io-item">
                <span className="tx-address mono" title={inp.public_key}>
                  {shortHash(inp.public_key)}
                </span>
                <span className="mono" style={{ fontSize: '0.72rem', color: 'var(--text-secondary)' }}>
                  #{inp.vout}
                </span>
              </div>
            ))
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

function BlockItem({ block }: { block: Block }) {
  const [open, setOpen] = useState(false);

  return (
    <div className="block-item">
      <div className="block-header" onClick={() => setOpen(!open)}>
        <div className="block-height">
          <Layers size={16} />
          Block #{block.index}
        </div>
        <div className="block-meta">
          <span className="hide-mobile">
            <Clock size={12} style={{ verticalAlign: 'middle' }} /> {formatTime(block.timestamp)}
          </span>
          <span>{block.transactions?.length ?? 0} tx</span>
          <ChevronDown size={18} className={`chevron ${open ? 'open' : ''}`} />
        </div>
      </div>

      {open && (
        <div className="block-details">
          <div className="block-detail-row">
            <span className="block-detail-label">Hash</span>
            <span className="block-detail-value mono">{shortHash(block.hash)}</span>
          </div>
          <div className="block-detail-row">
            <span className="block-detail-label">Previous Hash</span>
            <span className="block-detail-value mono">{shortHash(block.prev_hash)}</span>
          </div>
          <div className="block-detail-row">
            <span className="block-detail-label">Merkle Root</span>
            <span className="block-detail-value mono">{shortHash(block.merkle_root)}</span>
          </div>
          <div className="block-detail-row">
            <span className="block-detail-label">Nonce</span>
            <span className="block-detail-value">{block.nonce.toLocaleString()}</span>
          </div>
          <div className="block-detail-row">
            <span className="block-detail-label">Difficulty</span>
            <span className="block-detail-value">{block.difficulty}</span>
          </div>
          <div className="block-detail-row">
            <span className="block-detail-label">Timestamp</span>
            <span className="block-detail-value">{formatTime(block.timestamp)}</span>
          </div>

          {/* Transactions */}
          {block.transactions && block.transactions.length > 0 && (
            <div style={{ marginTop: 16 }}>
              <div style={{ fontSize: '0.82rem', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8 }}>
                <Hash size={14} style={{ verticalAlign: 'middle' }} /> Transactions ({block.transactions.length})
              </div>
              {block.transactions.map((tx, i) => (
                <TxItem key={tx.id || i} tx={tx} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function ExplorerPage() {
  const fetcher = useCallback(() => api.chain(), []);
  const { data: chain, loading, error, refetch } = useApi<Chain>(fetcher);

  // Pagination
  const [page, setPage] = useState(0);
  const perPage = 15;

  if (loading && !chain) return <LoadingState message="Loading blockchain..." />;
  if (error && !chain) return <ErrorState message={error} onRetry={refetch} />;
  if (!chain) return null;

  const blocks = [...(chain.blocks || [])].reverse();
  const totalPages = Math.ceil(blocks.length / perPage);
  const visibleBlocks = blocks.slice(page * perPage, (page + 1) * perPage);

  return (
    <div>
      <div className="page-header">
        <h1>Chain Explorer</h1>
        <p>
          {blocks.length} blocks &middot; {chain.total_mined.toLocaleString()} N mined
          <button
            className="btn btn-ghost btn-sm"
            onClick={refetch}
            style={{ marginLeft: 12, verticalAlign: 'middle' }}
          >
            <RefreshCw size={14} />
          </button>
        </p>
      </div>

      {blocks.length === 0 ? (
        <EmptyState icon={Blocks} message="No blocks in chain yet" />
      ) : (
        <>
          <div className="block-list">
            {visibleBlocks.map((block) => (
              <BlockItem key={block.index} block={block} />
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div style={{ display: 'flex', justifyContent: 'center', gap: 8, marginTop: 24 }}>
              <button
                className="btn btn-ghost btn-sm"
                disabled={page === 0}
                onClick={() => setPage(page - 1)}
              >
                Previous
              </button>
              <span style={{ padding: '8px 16px', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                Page {page + 1} of {totalPages}
              </span>
              <button
                className="btn btn-ghost btn-sm"
                disabled={page >= totalPages - 1}
                onClick={() => setPage(page + 1)}
              >
                Next
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
