"use client";

import { useCallback, useState } from "react";

import { Modal } from "./Modal";

type ConfirmOptions = {
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
};

export function useConfirm() {
  const [state, setState] = useState<
    (ConfirmOptions & { resolve: (value: boolean) => void }) | null
  >(null);

  const confirm = useCallback((options: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      setState({ ...options, resolve });
    });
  }, []);

  const element = state ? (
    <Modal
      open
      onClose={() => {
        state.resolve(false);
        setState(null);
      }}
      title={state.title}
      size="sm"
      footer={
        <>
          <button
            type="button"
            className="sb-btn"
            data-variant="secondary"
            onClick={() => {
              state.resolve(false);
              setState(null);
            }}
          >
            {state.cancelLabel ?? "取消"}
          </button>
          <button
            type="button"
            className="sb-btn"
            data-variant={state.danger ? "danger" : "primary"}
            onClick={() => {
              state.resolve(true);
              setState(null);
            }}
          >
            {state.confirmLabel ?? "确认"}
          </button>
        </>
      }
    >
      <p className="text-[13px] leading-[20px] text-[var(--sb-text-2)]">
        {state.description ?? "该操作不可撤销。"}
      </p>
    </Modal>
  ) : null;

  return { confirm, confirmElement: element };
}
