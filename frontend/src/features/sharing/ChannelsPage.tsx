import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { EmptyState } from '@/components/ui/EmptyState';
import { useAuthStore } from '@/stores';
import { sharingApi, sharingPath } from '@/services/api/sharing';
import { apiClient } from '@/services/api/client';
import { useChannelStore } from './useChannelStore';
import { LoadState, PageHeader } from './shared';
import { date, number, useSharingActions, useSharingData } from './hooks';
import styles from './Sharing.module.scss';
import type { Channel, MarketplaceModel } from './types';
import type { SharedCredential } from '@/services/api/sharingCredentials';

function ChannelCard({
  channel,
  credentials,
  models,
}: {
  channel: Channel;
  credentials: SharedCredential[];
  models: MarketplaceModel[];
}) {
  const { t } = useTranslation();
  const mounted = credentials.filter((file) => file.channel_id === channel.id);
  const credentialCount = mounted.filter((file) => file.kind !== 'provider').length;
  const providerCount = mounted.filter((file) => file.kind === 'provider').length;
  const channelModels = models.filter((model) => model.channel_id === channel.id);
  const availableCount = channelModels.filter((model) => model.available).length;

  return (
    <Link
      to={'/channels/' + channel.id}
      className={styles.channelLink}
      aria-label={t('sharing.channel_detail') + ': ' + channel.slug}
    >
      <Card
        className={styles.channelCard}
        title={channel.slug}
        extra={
          <span
            className={`${styles.status} ${channel.published ? styles.active : styles.inactive}`}
          >
            <span className={styles.statusDot} aria-hidden="true" />
            {t(channel.published ? 'sharing.published' : 'sharing.withdrawn')}
          </span>
        }
      >
        <div className={styles.channelBody}>
          <div className={styles.cardTags}>
            <span className={styles.tag}>
              {t(channel.public ? 'sharing.public_channel' : 'sharing.private_channel')}
            </span>
          </div>
          <dl className={styles.fieldGrid}>
            <div className={styles.field}>
              <dt>{t('sharing.owner_label')}</dt>
              <dd>{channel.owner}</dd>
            </div>
            <div className={styles.field}>
              <dt>{t('sharing.resources_label')}</dt>
              <dd>
                {t('sharing.resources_summary', {
                  credentials: credentialCount,
                  providers: providerCount,
                })}
              </dd>
            </div>
            <div className={styles.field}>
              <dt>{t('sharing.callable_models')}</dt>
              <dd>
                {number(availableCount)}
                <span className={styles.tagMeta}>
                  {' / '}
                  {t('sharing.callable_models_hint', { total: channelModels.length })}
                </span>
              </dd>
            </div>
            <div className={styles.field}>
              <dt>{t('sharing.created_at_label')}</dt>
              <dd>{date(channel.created_at)}</dd>
            </div>
          </dl>
          {channel.announcement && (
            <p className={styles.announcementSnippet}>{channel.announcement}</p>
          )}
        </div>
      </Card>
    </Link>
  );
}

export function ChannelsPage() {
  const { t } = useTranslation();
  const account = useAuthStore((state) => state.account);
  const { data, loading, error, refresh } = useSharingData(
    useCallback(async () => {
      const [channels, credentials, marketplace] = await Promise.all([
        sharingApi.channels(),
        apiClient.get<{ credentials: SharedCredential[] }>(sharingPath('/credentials')),
        sharingApi.marketplace(),
      ]);
      return { ...channels, ...credentials, models: marketplace.models };
    }, [])
  );
  const { run, confirm } = useSharingActions(refresh);
  const [creating, setCreating] = useState(false);
  const [slug, setSlug] = useState('');
  const [isPublic, setIsPublic] = useState(false);
  const [saving, setSaving] = useState(false);
  const save = async () => {
    setSaving(true);
    await run(async () => {
      await sharingApi.createChannel({ slug, public: isPublic });
      await useChannelStore.getState().load();
      setCreating(false);
    });
    setSaving(false);
  };
  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.channels')}
        hint={t('sharing.channels_hint')}
        actions={
          <Button
            onClick={() => {
              setSlug('');
              setIsPublic(false);
              setCreating(true);
            }}
          >
            {t('sharing.create_channel')}
          </Button>
        }
      />
      <LoadState loading={loading} error={error} />
      <div className={styles.grid}>
        {data?.channels.map((channel) => (
          <ChannelCard
            key={channel.id}
            channel={channel}
            credentials={data.credentials}
            models={data.models}
          />
        ))}
      </div>
      {!loading && !data?.channels.length && <EmptyState title={t('sharing.no_channels')} />}
      {account?.role === 'admin' && data?.credentials.some((file) => file.claimable) && (
        <Card title={t('sharing.unclaimed')}>
          {data.credentials
            .filter((file) => file.claimable)
            .map((file) => (
              <div className={styles.actions} key={file.id}>
                <span>{file.name}</span>
                {data.channels.map((channel) => (
                  <Button
                    key={channel.id}
                    variant="secondary"
                    size="sm"
                    onClick={() =>
                      confirm(t('sharing.claim_confirm'), () =>
                        sharingApi.claim(file.route_id, channel.id)
                      )
                    }
                  >
                    {t('sharing.claim_to', { channel: channel.slug })}
                  </Button>
                ))}
              </div>
            ))}
        </Card>
      )}
      <Modal
        open={creating}
        onClose={() => setCreating(false)}
        title={t('sharing.create_channel')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreating(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              onClick={() => void save()}
              loading={saving}
              disabled={!/^[a-z0-9][a-z0-9-]{0,39}$/.test(slug)}
            >
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Input
          label={t('sharing.channel_slug')}
          value={slug}
          onChange={(event) => setSlug(event.target.value)}
          maxLength={40}
          pattern="[a-z0-9][a-z0-9-]{0,39}"
          hint={t('sharing.channel_slug_hint')}
        />
        <ToggleSwitch
          checked={isPublic}
          onChange={setIsPublic}
          label={t('sharing.public_channel')}
        />
        <p className={styles.meta}>{t('sharing.public_channel_hint')}</p>
      </Modal>
    </div>
  );
}
