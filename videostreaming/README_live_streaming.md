# Live intégré au placement Go et aux signatures V3

Le [guide opérationnel et les preuves du 20/10](../docs/live-v4/LIVRAISON.md) détaille Windows, OBS, lecture, mesures et préparation du laboratoire.

```powershell
./deploy/live-v4/start.ps1
./deploy/live-v4/smoke.ps1
./deploy/live-v4/publish.ps1 -Email secure-owner-a@example.invalid -Title 'Direct'
```

Lecteur : `http://localhost:20690/live/`. OBS : `rtmp://127.0.0.1:21935/live`, clé `stream`, H.264/AAC, GOP 2 s. RTMP local est une source de confiance, pas un endpoint public authentifié. Comptes de démonstration et limites : voir le guide.

`live_publish.py` publie les fragments par l'agent propriétaire authentifié, ses clés persistantes, les autorités V3 et les uploads existants. Go/SQLite/Zone Controllers gardent seuls le placement. Le lecteur fait reconstruire et vérifier sur les vrais Edge. `live_watcher.py` est une entrée vers le même programme, avec des arguments explicites différents du prototype.

`live_ingest.sh` et `live_verify.py` restent des outils historiques ; leur format placement.json/signature n'est pas celui de V3. Code conservé ; ancien watcher récupérable dans Git. Ne pas mélanger formats ou clés d'essai avec le parcours intégré.

Le test réel est `deploy/live-v4/live-check.py`, piloté avec Chromium par `browser-check.cjs`. Source synthétique, transport RTMP/fichiers Edge/calculs crypto réels. SSIM/PSNR exigent une référence conservée ; l'âge programme n'est pas caméra → écran. Aucun essai physique Pi/Jetson affirmé.
