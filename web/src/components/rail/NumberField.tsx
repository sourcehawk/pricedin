"use client";

import { useState, type InputHTMLAttributes } from "react";

type InputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "value" | "onChange" | "type" | "defaultValue"
>;

/**
 * NumberField is a text field for a number that another control can also
 * change. While it has focus it keeps what the user types; each text that
 * parses to a finite number goes to onValue. Without focus it shows text,
 * the current value. On blur, an empty field calls onEmpty when given.
 */
export function NumberField({
  text,
  onValue,
  onEmpty,
  ...props
}: InputProps & {
  text: string;
  onValue: (v: number) => void;
  onEmpty?: () => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  return (
    <input
      {...props}
      type="number"
      value={draft ?? text}
      onFocus={(e) => {
        setDraft(text);
        props.onFocus?.(e);
      }}
      onChange={(e) => {
        const raw = e.target.value;
        setDraft(raw);
        const v = Number.parseFloat(raw);
        if (raw.trim() !== "" && Number.isFinite(v)) {
          onValue(v);
        }
      }}
      onBlur={(e) => {
        if (draft !== null && draft.trim() === "") {
          onEmpty?.();
        }
        setDraft(null);
        props.onBlur?.(e);
      }}
    />
  );
}
