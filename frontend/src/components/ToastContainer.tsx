import { useToast } from '../context/AppContext';
import { CheckCircle, XCircle, Info, AlertTriangle, X } from 'lucide-react';

const icons = {
  success: CheckCircle,
  error: XCircle,
  info: Info,
  warning: AlertTriangle,
};

export default function ToastContainer() {
  const { toasts, removeToast } = useToast();

  if (toasts.length === 0) return null;

  return (
    <div className="toast-container">
      {toasts.map((toast) => {
        const Icon = icons[toast.type];
        return (
          <div key={toast.id} className={`toast ${toast.type}`}>
            <Icon className="toast-icon" />
            <span style={{ flex: 1 }}>{toast.message}</span>
            <button className="toast-close" onClick={() => removeToast(toast.id)}>
              <X size={16} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
