import React, { createContext, useContext, useState, useCallback, useEffect } from 'react';
import type { KeyPair } from '../services/api';

// ─── Wallet Context ──────────────────────────────────────────────────

interface WalletState {
  wallet: KeyPair | null;
  setWallet: (kp: KeyPair) => void;
  clearWallet: () => void;
}

const WalletContext = createContext<WalletState>({
  wallet: null,
  setWallet: () => {},
  clearWallet: () => {},
});

export function WalletProvider({ children }: { children: React.ReactNode }) {
  const [wallet, setWalletState] = useState<KeyPair | null>(() => {
    try {
      const saved = localStorage.getItem('noda_wallet');
      return saved ? JSON.parse(saved) : null;
    } catch {
      return null;
    }
  });

  const setWallet = useCallback((kp: KeyPair) => {
    setWalletState(kp);
    localStorage.setItem('noda_wallet', JSON.stringify(kp));
    console.log('[Wallet] Saved wallet to localStorage');
  }, []);

  const clearWallet = useCallback(() => {
    setWalletState(null);
    localStorage.removeItem('noda_wallet');
    console.log('[Wallet] Cleared wallet');
  }, []);

  return (
    <WalletContext.Provider value={{ wallet, setWallet, clearWallet }}>
      {children}
    </WalletContext.Provider>
  );
}

export const useWallet = () => useContext(WalletContext);

// ─── Toast Context ───────────────────────────────────────────────────

export type ToastType = 'success' | 'error' | 'info' | 'warning';

export interface Toast {
  id: string;
  message: string;
  type: ToastType;
}

interface ToastState {
  toasts: Toast[];
  addToast: (message: string, type?: ToastType) => void;
  removeToast: (id: string) => void;
}

const ToastContext = createContext<ToastState>({
  toasts: [],
  addToast: () => {},
  removeToast: () => {},
});

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const addToast = useCallback(
    (message: string, type: ToastType = 'info') => {
      const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
      setToasts((prev) => [...prev, { id, message, type }]);
      setTimeout(() => removeToast(id), 4500);
    },
    [removeToast]
  );

  return (
    <ToastContext.Provider value={{ toasts, addToast, removeToast }}>
      {children}
    </ToastContext.Provider>
  );
}

export const useToast = () => useContext(ToastContext);

// ─── Combined Provider ───────────────────────────────────────────────

export function AppProviders({ children }: { children: React.ReactNode }) {
  // Prevent any flash before dark mode kicks in
  useEffect(() => {
    document.documentElement.classList.add('dark');
  }, []);

  return (
    <ToastProvider>
      <WalletProvider>{children}</WalletProvider>
    </ToastProvider>
  );
}
