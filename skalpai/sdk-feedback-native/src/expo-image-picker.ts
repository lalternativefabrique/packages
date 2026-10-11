import * as ImagePicker from 'expo-image-picker';
import type { FeedbackAttachment } from './submit';

const EXTENSION_BY_MIME: Record<string, string> = {
  'image/png': 'png',
  'image/jpeg': 'jpg',
  'image/webp': 'webp',
  'image/gif': 'gif',
};

export async function pickImagesWithExpo(remaining: number): Promise<FeedbackAttachment[]> {
  const result = await ImagePicker.launchImageLibraryAsync({
    mediaTypes: ['images'],
    allowsMultipleSelection: remaining > 1,
    selectionLimit: remaining,
    quality: 0.85,
    // HEIC is what iOS stores by default and the server only accepts web formats.
    preferredAssetRepresentationMode: ImagePicker.UIImagePickerPreferredAssetRepresentationMode.Compatible,
  });
  if (result.canceled) return [];
  return result.assets.map((asset, i) => {
    const mimeType = asset.mimeType ?? 'image/jpeg';
    return {
      uri: asset.uri,
      name: `image-${i + 1}.${EXTENSION_BY_MIME[mimeType] ?? 'jpg'}`,
      mimeType,
      size: asset.fileSize,
    };
  });
}
