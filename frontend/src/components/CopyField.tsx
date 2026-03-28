import { Copy, Check } from 'lucide-react';
import { useCopyToClipboard } from '../hooks/useApi';
import { useToast } from '../context/AppContext';

interface Props {
  text: string;
  label?: string;
  mono?: boolean;
  blurred?: boolean;
}

export default function CopyField({ text, label, mono = true, blurred = false }: Props) {
  const { copied, copy } = useCopyToClipboard();
  const { addToast } = useToast();

  const handleCopy = () => {
    copy(text);
    addToast(label ? `${label} copied!` : 'Copied to clipboard!', 'success');
  };

  return (
    <div className="address-display">
      <span
        className={`address-text ${mono ? 'mono' : ''} ${blurred ? 'private-key-blur' : ''}`}
        title={text}
      >
        {text}
      </span>
      <button className={`copy-btn ${copied ? 'copied' : ''}`} onClick={handleCopy} title="Copy">
        {copied ? <Check size={16} /> : <Copy size={16} />}
      </button>
    </div>
  );
}
