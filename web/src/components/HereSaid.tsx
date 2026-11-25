import { useEffect } from 'react';
import { useHere } from '../here';
import { useToast } from './Toast';

/** What went wrong with a song of this device, on any page. */
export function HereSaid() {
  const { said } = useHere();
  const [toast, say] = useToast();
  useEffect(() => { if (said) say({ tone: 'bad', text: said.text }); }, [said]); // eslint-disable-line react-hooks/exhaustive-deps
  return toast;
}
