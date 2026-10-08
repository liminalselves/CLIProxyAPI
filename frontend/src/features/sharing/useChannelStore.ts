import { create } from 'zustand';
import { sharingApi } from '@/services/api/sharing';
import { useAuthStore } from '@/stores/useAuthStore';
import type { Channel } from './types';

interface ChannelState {
  channels: Channel[];
  selected: string;
  load: () => Promise<void>;
  select: (id: string) => void;
  clear: () => void;
}

export const useChannelStore = create<ChannelState>((set, get) => ({
  channels: [],
  selected: '',
  select: (id) => set({ selected: id }),
  clear: () => set({ channels: [], selected: '' }),
  load: async () => {
    const identity = useAuthStore.getState().managementKey;
    const response = await sharingApi.channels();
    const account = useAuthStore.getState().account;
    if (identity !== useAuthStore.getState().managementKey || !account) return;
    const selectable = response.channels.filter(
      (channel) => channel.owner === account.username || account.role === 'admin' || channel.public
    );
    set({
      channels: response.channels,
      selected: selectable.some((channel) => channel.id === get().selected)
        ? get().selected
        : (selectable[0]?.id ?? ''),
    });
  },
}));
