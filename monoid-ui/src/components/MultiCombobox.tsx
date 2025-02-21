import React from 'react';
import { FormatOptionLabelMeta } from 'react-select';
import AsyncSelect from 'react-select/async';
import { StyledSelectInput } from './TextMultiInput';

interface BaseComboboxProps<T> extends Omit<Omit<Omit<React.HTMLProps<HTMLDivElement>, 'id'>, 'value'>, 'onChange'> {
  filter: (query: string) => Promise<T[]>,
  id: (v: T) => string,
  displayNode: (v: T, meta: FormatOptionLabelMeta<T>) => React.ReactNode,
  menuPortalTarget?: HTMLElement | null
}

type ComboboxProps<T> = ({
  value: T[] | undefined,
  onChange: (v: readonly T[]) => void,
  isMulti: true
} | {
  value: T | undefined,
  onChange: (v?: T | null) => void,
  isMulti: false
}) & BaseComboboxProps<T>;

export default function Combobox<T>(props: ComboboxProps<T>) {
  const {
    value, onChange, filter, id, displayNode, className, isMulti, menuPortalTarget,
  } = props;

  if (isMulti) {
    return (
      <AsyncSelect
        components={{
          Input: StyledSelectInput,
        }}
        loadOptions={filter}
        value={value}
        formatOptionLabel={(v, meta) => displayNode(v, meta)}
        getOptionValue={(v) => id(v)}
        onChange={(v) => onChange(v)}
        className={className}
        menuPortalTarget={menuPortalTarget}
        isMulti
        defaultOptions
        styles={{
          option: (base, state) => ({
            ...base,
            backgroundColor: state.isFocused ? 'rgb(243 244 246)' : undefined,
            color: state.isFocused ? 'rgb(255 255 255)' : undefined,
            '&:active': {
              backgroundColor: 'rgb(243 244 246)',
            },
          }),
        }}
      />
    );
  }

  return (
    <AsyncSelect
      components={{
        Input: StyledSelectInput,
      }}
      loadOptions={filter}
      value={value}
      formatOptionLabel={(v, meta) => displayNode(v, meta)}
      getOptionValue={(v) => id(v)}
      onChange={(v) => onChange(v)}
      menuPortalTarget={menuPortalTarget}
      styles={{
        option: (base, state) => ({
          ...base,
          backgroundColor: state.isFocused ? 'rgb(243 244 246)' : undefined,
          color: state.isFocused ? 'rgb(0 0 0)' : undefined,
          '&:active': {
            backgroundColor: 'rgb(243 244 246)',
          },
        }),
      }}
      defaultOptions
    />
  );
}

Combobox.defaultProps = {
  menuPortalTarget: undefined,
};
