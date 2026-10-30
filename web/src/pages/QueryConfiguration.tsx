import { DiscoveryPage } from './Discovery';

export function QueryConfiguration({ id, onSaved }: { id: string; onSaved: () => void }) {
 return <DiscoveryPage id={id} onSaved={onSaved} />;
}
