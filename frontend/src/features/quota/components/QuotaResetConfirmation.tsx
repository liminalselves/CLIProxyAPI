import { useState } from 'react';
import { Select, type SelectOption } from '@/components/ui/Select';

export function QuotaResetConfirmation({
  options,
  message,
  onChange,
}: {
  options: SelectOption[];
  message: string;
  onChange: (value: string) => void;
}) {
  const [value, setValue] = useState(options[0].value);
  return (
    <>
      <p>{message}</p>
      <Select
        value={value}
        options={options}
        ariaLabel={message}
        onChange={(selected) => {
          setValue(selected);
          onChange(selected);
        }}
      />
    </>
  );
}
