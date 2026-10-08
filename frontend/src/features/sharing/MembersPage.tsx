import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/Table';
import { sharingApi } from '@/services/api/sharing';
import { LoadState, PageHeader } from './shared';
import { useSharingData, useSharingActions, date } from './hooks';
import styles from './Sharing.module.scss';

export function MembersPage() {
  const { t } = useTranslation();
  const { data, loading, error, refresh } = useSharingData(
    useCallback(async () => {
      const [users, invites] = await Promise.all([sharingApi.users(), sharingApi.invites()]);
      return { ...users, ...invites };
    }, [])
  );
  const { run, confirm, copy } = useSharingActions(refresh);
  const [open, setOpen] = useState(false);
  const [uses, setUses] = useState('1');
  const [days, setDays] = useState('7');
  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.members')}
        hint={t('sharing.members_hint')}
        actions={<Button onClick={() => setOpen(true)}>{t('sharing.create_invite')}</Button>}
      />
      <LoadState loading={loading} error={error} />
      <Card title={t('sharing.members')}>
        <Table>
          <TableHeader>
            <TableRow>
              {['username', 'role', 'enabled', 'actions'].map((key) => (
                <TableHead key={key}>{t('sharing.' + key)}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.users.map((member) => (
              <TableRow key={member.username}>
                <TableCell>{member.username}</TableCell>
                <TableCell>{t('sharing.' + member.role)}</TableCell>
                <TableCell>{t(member.disabled ? 'sharing.disabled' : 'sharing.enabled')}</TableCell>
                <TableCell>
                  <div className={styles.actions}>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() =>
                        confirm(t('sharing.change_member_confirm'), () =>
                          sharingApi.updateUser(member.username, { disabled: !member.disabled })
                        )
                      }
                    >
                      {t(member.disabled ? 'sharing.enable' : 'sharing.disable')}
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() =>
                        confirm(t('sharing.change_member_confirm'), () =>
                          sharingApi.updateUser(member.username, {
                            role: member.role === 'admin' ? 'member' : 'admin',
                          })
                        )
                      }
                    >
                      {t(member.role === 'admin' ? 'sharing.make_member' : 'sharing.make_admin')}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
      <Card title={t('sharing.invites')}>
        <Table>
          <TableHeader>
            <TableRow>
              {['invite', 'uses', 'expires', 'invited_users', 'actions'].map((key) => (
                <TableHead key={key}>{t('sharing.' + key)}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.invites.map((invite) => (
              <TableRow key={invite.id}>
                <TableCell className={styles.mono}>{invite.code.slice(0, 20)}…</TableCell>
                <TableCell>
                  {invite.used_by.length} / {invite.max_uses}
                </TableCell>
                <TableCell>{date(invite.expires_at)}</TableCell>
                <TableCell>{invite.used_by.join(', ') || '—'}</TableCell>
                <TableCell>
                  <div className={styles.actions}>
                    <Button variant="secondary" size="sm" onClick={() => void copy(invite.code)}>
                      {t('common.copy')}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      disabled={invite.revoked}
                      onClick={() =>
                        confirm(t('sharing.revoke_invite_confirm'), () =>
                          sharingApi.revokeInvite(invite.id)
                        )
                      }
                    >
                      {t('sharing.revoke')}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title={t('sharing.create_invite')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              onClick={() =>
                void run(async () => {
                  const result = await sharingApi.createInvite(Number(uses), Number(days));
                  await copy(result.invite.code);
                  setOpen(false);
                })
              }
            >
              {t('sharing.create_invite')}
            </Button>
          </>
        }
      >
        <Input
          label={t('sharing.max_uses')}
          type="number"
          min={1}
          max={100}
          value={uses}
          onChange={(event) => setUses(event.target.value)}
        />
        <Input
          label={t('sharing.valid_days')}
          type="number"
          min={1}
          max={365}
          value={days}
          onChange={(event) => setDays(event.target.value)}
        />
      </Modal>
    </div>
  );
}
