import jsQR from 'jsqr'

// Decode every image-based activation input through one browser path so file,
// paste, and drag/drop behavior stay consistent.
export async function decodeEsimActivationImage(file: File): Promise<string | null> {
  const url = URL.createObjectURL(file)
  try {
    const image = new Image()
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('image load failed'))
      image.src = url
    })
    const canvas = document.createElement('canvas')
    canvas.width = image.naturalWidth
    canvas.height = image.naturalHeight
    const context = canvas.getContext('2d')
    if (!context) return null
    context.drawImage(image, 0, 0)
    const imageData = context.getImageData(0, 0, canvas.width, canvas.height)
    return jsQR(imageData.data, imageData.width, imageData.height)?.data || null
  } catch {
    return null
  } finally {
    URL.revokeObjectURL(url)
  }
}
