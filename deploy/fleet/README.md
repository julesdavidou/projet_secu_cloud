# Outils CaribouTV

La source des outils est versionnée dans `projet_secu_cloud/deploy/fleet` pour les tests CI. La copie d’utilisation, `inventory.local.json`, le venv et les rapports restent dans le dossier voisin `cariboutv-outils`, hors Git.

Guide complet : `projet_secu_cloud/docs/deployment-ghcr.md`.

Après un pull du dépôt, synchroniser les outils sans écraser les accès et les rapports :

```powershell
cd 'C:\Users\jules\Documents\ICY\5A\Secu Cloud\projet_secu_cloud'
.\deploy\fleet\install-tools.ps1
cd '..\cariboutv-outils'
```

Après publication des images par GitHub Actions :

```powershell
.\deploy.ps1 -RegistryUser julesdavidou -CoordinatorAddress 192.168.2.64 -ConfigureFirewall -Jobs 5
.\test.ps1 -HealthOnly
.\test.ps1
```

Le mode par défaut est désormais **registry**. Le PAT classic `read:packages` est demandé en saisie masquée, ou lu dans `CARIBOU_GHCR_TOKEN` fourni par un gestionnaire de secrets. Ne jamais le placer dans Git, les commandes, l’inventaire ou le chat. `CARIBOU_GHCR_USERNAME` peut fournir le compte GitHub.

Avant la première publication ou en cas d’indisponibilité GHCR :

```powershell
.\deploy.ps1 -ImageSource local -CoordinatorAddress 192.168.2.64 -ConfigureFirewall -Jobs 5
```

`-Nodes jetson02,raspberry04,raspberry05 -Jobs 3` sélectionne le groupe actif complet : les autres machines ne sont ni contactées ni arrêtées et sont exclues de la nouvelle topologie centrale. Sans `-Nodes`, le déploiement cible les cinq. Les tests reprennent le dernier groupe demandé ; leur option `-Nodes` ne reconfigure pas le scheduler.

`-CheckOnly` contrôle SSH/sudo/Docker/architecture/horloge/espace/Compose uniquement : aucun déploiement, aucune installation de Compose, aucun accès GHCR. `-SkipFetch` garde la référence Git locale ; `-SkipBuild` concerne le mode local et exige des images du bon commit. Le code non committé n’est pas déployé. `-Revision SHA_COMPLET` sélectionne un commit précis ; `-ForceRecreate` recrée ses conteneurs même si l’image est inchangée.

Compose est automatiquement installé si nécessaire dans `~/cariboutv-lab/tools/docker-compose` (minimum Edge 2.20, PC 2.24.4+). Le dossier distant dépend du compte SSH, par exemple `/home/camera/cariboutv-lab`. Il n’est créé qu’au déploiement.

```bash
cd ~/cariboutv-lab/deployment
sudo docker compose -p caribou-fleet -f compose.edge.yaml ps -a
sudo docker compose -p caribou-fleet -f compose.edge.yaml logs --tail 100
```

Si Compose dédié est installé, remplacer `docker compose` par `~/cariboutv-lab/tools/docker-compose`.

Le PC héberge les services centraux et le broker du banc sur TCP 1884 ; le broker tiers 192.168.1.1:1883 n’est pas utilisé ici. Les Edge publient leur API sur 18081. Le site est sur `http://IP_DU_PC:18090/`, la console sur `/integration/`. Le PC reste allumé.

Les rapports sont dans `.runtime/reports`. Les volumes, ledgers, secrets, quotas et anciennes releases sont conservés. Un échec d’activation tente de restaurer la configuration précédente du nœud ; ce n’est pas un rollback atomique du parc ou des données.

`test.ps1` crée un compte et une courte vidéo privée conservés dans le stockage. `-HealthOnly` ne crée aucun média. Tous les nœuds sélectionnés sont vérifiés pour santé/version/heartbeats ; seuls ceux choisis par le scheduler transportent la vidéo.

Tests unitaires sans matériel : `py -3 -m unittest discover -s . -p 'test_*.py' -v`.
