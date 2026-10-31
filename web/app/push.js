// Web push: the browser's subscription and registering it with the server.
// The receiving side is web/sw.js.

import { call } from "./api.js";
import { state } from "./state.js";
import { render } from "./shell.js";

// ---- Settings: push notifications -----------------------------------------------------

export function pushSupported() {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

// VAPID applicationServerKey must be a Uint8Array; the server hands it over
// as the base64url string PushManager.subscribe() itself can't consume.
function urlBase64ToUint8Array(base64String) {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const rawData = atob(base64);
  const output = new Uint8Array(rawData.length);
  for (let i = 0; i < rawData.length; i++) output[i] = rawData.charCodeAt(i);
  return output;
}

async function getCurrentPushSubscription() {
  if (!pushSupported()) return null;
  const reg = await navigator.serviceWorker.ready;
  return reg.pushManager.getSubscription();
}

// undefined = not checked yet, null = checked and not subscribed, object =
// subscribed. Module-level (not in `state`) since it's derived from the
// browser's own PushManager, not server data.
export let cachedPushSubscription;
let pushSubscriptionCheckInFlight = false;

export function refreshPushSubscriptionCache() {
  if (pushSubscriptionCheckInFlight) return;
  pushSubscriptionCheckInFlight = true;
  getCurrentPushSubscription()
    .then((sub) => {
      cachedPushSubscription = sub || null;
    })
    .catch(() => {
      cachedPushSubscription = null;
    })
    .finally(() => {
      pushSubscriptionCheckInFlight = false;
      if (state.tab === "settings") render();
    });
}

export async function enablePushNotifications() {
  if (Notification.permission === "denied") throw new Error(t("settings.notificationsDenied"));
  const permission = await Notification.requestPermission();
  if (permission !== "granted") throw new Error(t("settings.notificationsDenied"));
  if (!state.pushConfig || !state.pushConfig.vapidPublicKey) throw new Error(t("settings.notificationsUnavailable"));

  const reg = await navigator.serviceWorker.ready;
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(state.pushConfig.vapidPublicKey),
  });
  const subJson = sub.toJSON();
  await call("SubscribeToPush", {
    userId: state.userId,
    subscription: { endpoint: subJson.endpoint, p256dh: subJson.keys.p256dh, auth: subJson.keys.auth },
  });
  cachedPushSubscription = sub;
}

export async function disablePushNotifications() {
  const sub = await getCurrentPushSubscription();
  if (sub) {
    await call("UnsubscribeFromPush", { endpoint: sub.endpoint });
    await sub.unsubscribe();
  }
  cachedPushSubscription = null;
}
