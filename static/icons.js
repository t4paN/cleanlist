// The switch is the only thing on this page that is not a plain form post.
// Reloading afterwards is deliberate: the icons are rendered server-side, so the
// page has to come back to show the change.
document.getElementById('opt-icons').onchange = async (e) => {
  const on = e.target.checked;
  try {
    const r = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ custom_icons: on }),
    });
    if (!r.ok) throw new Error('could not save the setting');
    location.reload();
  } catch (err) {
    e.target.checked = !on;
    alert(err.message);
  }
};
