# Laboratorio 2
## Integrantes
    Pablo Antonio Retamales Jara            ROL: 202173650-6
    Geraldine Fernanda Cornejo Valenzuela   ROL: 202173529-1

## Consideraciones
    * Al ser tantos entrenadores simulados, es muy probable que te ganen
    el puesto en inscribirse, por este se le añadió un time sleep, 
    que no lo soluciona del todo, por esto es que hay que ser rápido
    al intentar inscribirse a un torneo.

    * Consideraremos máximo 5 regiones como las existentes,
    dentro de las que se definió un gimnasio para cada región:
    Kanto, Johto, Hoenn, Sinnoh, Unova

    * Consideramos que las notificaciones o alertas se muestran, pero
    al seleccionar "Ver notificaciones recibidas en el menú mostrará
    las últimas 5 notificaciones guardadas, para no colapsar la terminal.

## Orden de ejecución
    El orden sugerido para la ejecución es el siguiente:
    gimnasios-snp -> lcp -> cdp -> entrenadores

## Distribución de las VMs
    dist061 -> gimnasios - snp
    dist062 -> lcp
    dist063 -> cdp
    dist064 -> entrenadores

    Es decir, el orden de ejecución es:
    dist061 -> dist062 -> dist063 -> dist064

    Obligatorio: debe esperar que se levante completamente el contenedor para que funcione.

## Instrucciones para ejecución
    Primero, debe ubicarse en la carpeta lab2/{entidad}, es decir, si ejecuta
    docker-lcp, entonces debe estar ubicado en lab2/lcp.

    Luego, basta con ejecutar el makefile, existen dos instrucciones en la que una
    es opcional.

    Solo es necesario ejecutar:
    (dist061) Usar "make docker-gimnasios-snp"
    (dist062) Usar "make docker-lcp"
    (dist063) Usar "make docker-cdp"
    (dist064) Usar "make docker-entrenadores"

    Esto solamente levantará el contenedor que se dejó previamente buildeado en la VM.
    En caso de necesitar ejecutar el build, es muy sencillo, se utiliza:

    (dist061) Usar "make docker-build-gimnasios-snp"
    (dist062) Usar "make docker-build-lcp"
    (dist063) Usar "make docker-build-cdp"
    (dist064) Usar "make docker-build-entrenadores"

    Recordar que esto no es necesario, solo es un paso opcional si quieres usar otra configuración
    como por ejemplo, cambiar el .json de entrenadores.

## Quiero cambiar el .json de entrenadores por defecto, ¿Cómo lo hago?
    El .json se define en el makefile dentro de la VM (dist064)
    "FILE_NAME ?= entrenadores_grande.json"

    Para cambiar a otro .json, sería modificar el nombre del archivo,
    hacer el build nuevamente y levantar el contenedor.

## docker-compose vs docker compose y sudo
    En versiones más recientes de Docker, el sudo docker-compose no funciona,
    la configuración del makefile, es para versiones más recientes de Docker.
    Versiones más recientes incluyen permisos para actuar sin sudo, por lo que funciona
    correcto con 'docker compose up ...' (Sin guión y sin usar sudo)

    En caso de error por usar una versión muy antigua:
    Puede cambiarse por 'sudo docker-compose up --build ...'

    (!) En las VMs, se respeta la versión antigua de Docker, usando
        'sudo docker-compose up ...'