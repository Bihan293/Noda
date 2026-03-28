import { Loader } from 'lucide-react';

interface Props {
  message?: string;
  size?: 'sm' | 'lg';
}

export function LoadingState({ message = 'Loading...', size = 'lg' }: Props) {
  return (
    <div className="loading-state">
      <div className={`spinner ${size}`} />
      <span>{message}</span>
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="error-state">
      <Loader size={32} />
      <p>{message}</p>
      {onRetry && (
        <button className="btn btn-ghost btn-sm" onClick={onRetry}>
          Try Again
        </button>
      )}
    </div>
  );
}

export function EmptyState({ icon: Icon, message }: { icon: React.ElementType; message: string }) {
  return (
    <div className="empty-state">
      <Icon />
      <p>{message}</p>
    </div>
  );
}
