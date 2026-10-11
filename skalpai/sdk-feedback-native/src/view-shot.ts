import { captureScreen } from 'react-native-view-shot';
import type { FeedbackAttachment } from './submit';

export async function captureScreenWithViewShot(): Promise<FeedbackAttachment> {
  const uri = await captureScreen({ format: 'jpg', quality: 0.85, result: 'tmpfile' });
  return { uri, name: 'screenshot.jpg', mimeType: 'image/jpeg' };
}
