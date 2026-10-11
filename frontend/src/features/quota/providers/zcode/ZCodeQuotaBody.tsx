import type { ZCodeQuotaState } from '@/types';
import type { QuotaBodyProps } from '../../types';
import { CodexQuotaBody } from '../codex/CodexQuotaBody';

export function ZCodeQuotaBody(props: QuotaBodyProps<ZCodeQuotaState>) {
  return <CodexQuotaBody {...props} provider="zcode" />;
}
