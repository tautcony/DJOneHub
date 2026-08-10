import { Modal } from 'ant-design-vue'

export interface DangerConfirmation {
  title: string
  detail?: string
  confirmLabel: string
  cancelLabel: string
}

export function confirmDanger(options: DangerConfirmation): Promise<boolean> {
  return new Promise((resolve) => {
    Modal.confirm({
      title: options.title,
      content: options.detail,
      okText: options.confirmLabel,
      okType: 'danger',
      cancelText: options.cancelLabel,
      centered: true,
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    })
  })
}
