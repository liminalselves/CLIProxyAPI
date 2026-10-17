import { describe, expect, test } from 'bun:test';
import {
  credentialModelsToRows,
  rowsToCredentialModels,
  validateCredentialModelRows,
} from '../src/features/sharing/credentialModelRows';
import type {
  CredentialAvailableModel,
  CredentialModelCapabilities,
  CredentialModelEntry,
} from '../src/services/api/credentialModelManagement';

const capabilities: CredentialModelCapabilities = {
  manual_models: true,
  aliases: true,
  thinking: true,
  max_context_length: true,
  modalities: true,
  is_compat: true,
  support_configuration_update: true,
  merge: true,
  reset: true,
};

const available: CredentialAvailableModel[] = [{ id: 'gpt-5' }, { id: 'gpt-4' }];

describe('credential model rows', () => {
  test('round-trips wire entries through the editor without losing untouched fields', () => {
    const wire: CredentialModelEntry[] = [
      {
        name: 'gpt-5',
        alias: 'my-model',
        'max-context-length': 200000,
        thinking: { levels: ['high'], zero_allowed: true, min: 1024 },
        'is-compat': false,
        'support-configuration-update': false,
        'input-modalities': ['text', 'image'],
        'output-modalities': ['text'],
      },
      { name: 'gpt-4' },
    ];
    const rows = credentialModelsToRows(wire);
    expect(rows[0].name).toBe('gpt-5');
    expect(rows[0].alias).toBe('my-model');
    expect(rows[0].maxContextLength).toBe('200000');
    expect(rows[0].isCompat).toBe(false);
    expect(rows[0].thinkingEnabled).toBe(true);
    expect(rows[0].thinkingLevels).toEqual(['high']);
    expect(rows[1].thinkingEnabled).toBe(false);
    // Untouched rows resend exactly what the backend returned, nothing more.
    expect(rowsToCredentialModels(rows, capabilities)).toEqual(wire);
  });

  test('omits untouched options so the backend keeps inheriting the catalog', () => {
    const rows = credentialModelsToRows([{ name: 'gpt-5', alias: 'x' }]);
    expect(rowsToCredentialModels(rows, capabilities)).toEqual([{ name: 'gpt-5', alias: 'x' }]);
    rows[0].maxContextLength = '123456';
    expect(rowsToCredentialModels(rows, capabilities)[0]['max-context-length']).toBe(123456);
    rows[0].maxContextLength = '';
    expect(rowsToCredentialModels(rows, capabilities)[0]['max-context-length']).toBeUndefined();
  });

  test('drops blank rows, trims names, and respects disabled capabilities', () => {
    const rows = credentialModelsToRows([]);
    rows.push({ name: '  ', alias: 'skipped' }, { name: ' gpt-5 ', alias: '  ' });
    rows[1].isCompat = true;
    rows[1].inputModalitiesText = 'text, image';
    rows[1].inputModalitiesTouched = true;
    const wire = rowsToCredentialModels(rows, {
      ...capabilities,
      modalities: false,
      is_compat: false,
    });
    expect(wire).toEqual([{ name: 'gpt-5' }]);
  });

  test('an explicit empty thinking object stays an override', () => {
    const rows = credentialModelsToRows([{ name: 'gpt-5', thinking: {} }]);
    expect(rows[0].thinkingEnabled).toBe(true);
    expect(rowsToCredentialModels(rows, capabilities)[0].thinking).toEqual({});
  });

  test('rejects duplicate public names and models outside the account catalog', () => {
    expect(
      validateCredentialModelRows(
        credentialModelsToRows([
          { name: 'gpt-5', alias: 'same' },
          { name: 'gpt-4', alias: 'SAME' },
        ]),
        capabilities,
        available
      )
    ).toEqual({ key: 'sharing.credential_models_duplicate', name: 'SAME' });
    expect(
      validateCredentialModelRows(
        credentialModelsToRows([{ name: 'gpt-5' }]),
        capabilities,
        available
      )
    ).toBeNull();
    expect(
      validateCredentialModelRows(
        credentialModelsToRows([{ name: 'unknown-model' }]),
        { ...capabilities, manual_models: false },
        available
      )
    ).toEqual({ key: 'sharing.credential_models_not_available', name: 'unknown-model' });
    // An unavailable catalog cannot prove entitlement; stay editable.
    expect(
      validateCredentialModelRows(
        credentialModelsToRows([{ name: 'unknown-model' }]),
        { ...capabilities, manual_models: false },
        []
      )
    ).toBeNull();
  });
});
