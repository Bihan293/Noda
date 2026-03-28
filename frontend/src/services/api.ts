/**
 * Noda API Service
 * Handles all communication with the Noda cryptocurrency node.
 * API Base URL is configurable via environment variable.
 */

const API_BASE = import.meta.env.VITE_API_URL || 'https://noda-fly-io.fly.dev';

console.log('[Noda API] Base URL:', API_BASE);

// ─── Types ───────────────────────────────────────────────────────────

export interface HealthResponse {
  status: string;
  node: string;
  version: string;
}

export interface KeyPair {
  address: string;
  public_key: string;
  private_key: string;
}

export interface BalanceResponse {
  address: string;
  balance: number;
  utxo_count: number;
}

export interface NodeStatus {
  port: string;
  version: string;
  block_height: number;
  chain_length: number;
  cumulative_work: string;
  peers: number;
  http_peers: number;
  p2p_peers: number;
  total_mined: number;
  block_reward: number;
  total_faucet: number;
  faucet_active: boolean;
  faucet_address?: string;
  faucet_balance?: number;
  faucet_remaining?: number;
  mempool_size: number;
  utxo_count: number;
  max_supply: number;
  mining_enabled: boolean;
  miner_address: string;
  last_mined_block_hash: string;
  blocks_mined_by_node: number;
  genesis_owner: string;
  tx_model: string;
  insecure_wallet_http: boolean;
}

export interface TxInput {
  txid: string;
  vout: number;
  signature: string;
  public_key: string;
}

export interface TxOutput {
  amount: number;
  address: string;
}

export interface Transaction {
  id: string;
  version: number;
  inputs: TxInput[] | null;
  outputs: TxOutput[];
  lock_time: number;
  coinbase_data?: string;
}

export interface Block {
  index: number;
  timestamp: string;
  hash: string;
  prev_hash: string;
  nonce: number;
  difficulty: number;
  merkle_root: string;
  transactions: Transaction[];
}

export interface Chain {
  blocks: Block[];
  height: number;
  total_mined: number;
  total_faucet: number;
}

export interface MempoolResponse {
  size: number;
  transactions: Transaction[];
}

export interface PeersResponse {
  peers: string[];
}

export interface FaucetResponse {
  message: string;
  to: string;
  amount: number;
  txid: string;
  status: string;
  confirmations: number;
  faucet_remaining: number;
}

export interface BroadcastResponse {
  message: string;
  txid: string;
  status: string;
  confirmations: number;
}

export interface ApiError {
  error: string;
}

// ─── Fetch wrapper ───────────────────────────────────────────────────

class NodaApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = 'NodaApiError';
    this.status = status;
  }
}

async function request<T>(
  endpoint: string,
  options?: RequestInit,
  timeoutMs = 30000
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const url = `${API_BASE}${endpoint}`;

  console.log(`[Noda API] ${options?.method || 'GET'} ${url}`);

  try {
    const response = await fetch(url, {
      ...options,
      signal: controller.signal,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    });

    const data = await response.json();

    if (!response.ok) {
      const errMsg = (data as ApiError).error || `HTTP ${response.status}`;
      console.error(`[Noda API] Error ${response.status}:`, errMsg);
      throw new NodaApiError(errMsg, response.status);
    }

    console.log(`[Noda API] Response from ${endpoint}:`, data);
    return data as T;
  } catch (err: unknown) {
    if (err instanceof NodaApiError) throw err;
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw new NodaApiError('Request timed out. Node may be sleeping — try again.', 408);
    }
    // CORS or network error
    const message =
      err instanceof Error ? err.message : 'Network error — check if the node is running.';
    console.error('[Noda API] Network error:', message);
    throw new NodaApiError(message, 0);
  } finally {
    clearTimeout(timer);
  }
}

// ─── API Methods ─────────────────────────────────────────────────────

export const api = {
  /** GET /health */
  health: () => request<HealthResponse>('/health'),

  /** GET /status */
  status: () => request<NodeStatus>('/status'),

  /** GET /generate-keys */
  generateKeys: () => request<KeyPair>('/generate-keys'),

  /** GET /balance?address=... */
  balance: (address: string) =>
    request<BalanceResponse>(`/balance?address=${encodeURIComponent(address)}`),

  /** GET /chain */
  chain: () => request<Chain>('/chain', undefined, 60000),

  /** GET /mempool */
  mempool: () => request<MempoolResponse>('/mempool'),

  /** GET /peers */
  peers: () => request<PeersResponse>('/peers'),

  /** POST /faucet */
  faucet: (to: string) =>
    request<FaucetResponse>('/faucet', {
      method: 'POST',
      body: JSON.stringify({ to }),
    }),

  /** POST /tx/broadcast — production-safe raw transaction */
  broadcastTx: (tx: {
    version: number;
    inputs: TxInput[];
    outputs: TxOutput[];
    lock_time: number;
  }) =>
    request<BroadcastResponse>('/tx/broadcast', {
      method: 'POST',
      body: JSON.stringify(tx),
    }),
};

export { NodaApiError };
export default api;
