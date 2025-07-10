# Laboratorio 1 - Sistemas Distribuidos - Grupo 16

### Integrantes - Grupo 16:
    Pablo Retamales         ROL  202173650-6    Grupo 16
    Geraldine Cornejo       ROl  202173529-1    Grupo 16
    
## Importante
    El orden de ejecución es importante, primero correr el gobierno y esperar que haga el build.
    Después se sugiere este orden, donde el último en cargar debe ser el cazarrecompensas.
    - Gobierno -> Marina -> Submundo -> Cazarrecompensas

    (!) Por enunciado, se define un tiempo de operación en cada máquina (timeOperations), lo mismo para el tiempo de transporte,
    tiempo de intento de captura, por ahora el tiempo de operación se guardó como 60 * 6 = 6 minutos en total, por lo que SOBREPASADO
    ESTE TIEMPO, y dependiendo de en que momento exacto se enciendan las máquinas, se van a tirar error entre sí al intentar comunicarse con una VM
    con desconectada, lo cual es el comportamiento normal, ya que se cierra después del tiempo de operación.

## VMs Designadas
    dist061 -> Gobierno
    dist062 -> Marina
    dist063 -> Submundo
    dist064 -> Cazarrecompensas

## Instrucciones en local:
    1. Situarse en la carpeta al nivel de docker-compose.yml y makefile.

    2. Si se está en local, simplemente correr en terminales separadas:
        make docker-{x}, donde x es la entidad que se está ejecutando x = {marina, submundo, gobierno, cazarrecompensas} 

    Luego, en el orden sugerido, abrir una terminal y correr separadamente
    idealmente, esperar un tiempo entre cada instrucción para así cargue y conecte todo correctamente:
        make docker-gobierno
        make docker-marina
        make docker-submundo
        make docker-cazarrecompensas

    Esto hará el build del container en específico.

## Instrucciones dentro de la VMs:
    1. Situarse en la carpeta grupo-16/lab1

    2. Correr en la terminal de la VM designada para el uso de la entidad x,
    Esto levanta el contenedor ya creado, por lo que si se desea hacer un rebuild (ver instrucción 3):
        make docker-{x}, donde x es la entidad que se está ejecutando x = {marina, submundo, gobierno, cazarrecompensas}
    
    Luego, en el orden sugerido,
        dist061 -> make docker-gobierno
        dist062 -> make docker-marina
        dist063 -> make docker-submundo
        dist064 -> make docker-cazarrecompensas

    3. Si se desea recompilar el docker, está la funcion del makefile
        make docker-build-{x}, donde x es la entidad que se está ejecutando x = {marina, submundo, gobierno, cazarrecompensas}

        dist061 -> make docker-build-gobierno
        dist062 -> make docker-build-marina
        dist063 -> make docker-build-submundo
        dist064 -> make docker-build-cazarrecompensas

## Consideraciones:
    * Se utilizó un número de 3 cazarrecompensas, si desea modificarlo, cambiar parametro 'replicas'
    en docker-compose.yml, ya sea dentro de la VM o en local, deberá hacer rebuild con 'make docker-build-{x}'

    * Cuando la marina confisca un pirata y envía el informe al gobierno,
    el gobierno no le comunica al cazarrecompensas que su reputación bajó,
    pero la recibe actualizada en la próxima vez que envía un informe.

    (2) El porcentaje elegido para realizar las redadas será 25%, esto debido a que 
    se define dos veces en el enunciado y consideramos el mejor para nuestro 
    desarrollo. Además, consideraremos piratas de nivel alto a ser incautados 
    por la marina y que un porcentaje mayor al 40% del total de piratas 
    vendidos al submundo se considera alta actividad del submundo.
    Debido al enunciado: "4. Si el pirata es de alto nivel y hay actividad
    ilegal, la Marina puede lanzar una redada para confiscarlo."

    * Se entiende como costo de operación como el costo de oportunidad, no
    que debemos considerar un descuento en el balance del cazarrecompensas.a

    * Solo el cazarrecompensas tendrá balance ($ BERRIES).

    * Si 2 o más cazarrecompensas entregan al submundo al mismo pirata, entonces
    el submundo el va a pagar a todos, ya que el submundo no tiene forma de acceder
    a la lista de piratas buscados actualizada, por lo tanto "confia" en el cazarrecompensas

## docker-compose vs docker compose y sudo

En versiones más recientes de Docker, el sudo docker-compose no funciona,
la configuración del makefile, es para versiones más recientes de Docker.
Versiones más recientes incluyen permisos para actuar sin sudo, por lo que funciona
correcto con 'docker compose up ...' (Sin guión y sin usar sudo)

En caso de error por usar una versión muy antigua:
Puede cambiarse por 'sudo docker-compose up --build ...'

(!) En las VMs, se respeta la versión antigua de Docker, usando
    'sudo docker-compose up ...'

/------------------------------------------------------------------/
        ___  _____    
        .'/,-Y"     "~-.  
        l.Y             ^.           
        /\               _\_      dou   
        i            ___/"   "\ 
        |          /"   "\   o !   
        l         ]     o !__./   
        \ _  _    \.___./    "~\  
        X \/ \            ___./  
        ( \ ___.   _..--~~"   ~`-.  
        ` Z,--   /               \    
            \__.  (   /       ______) 
            \   l  /-----~~" /      
            Y   \          / 
            |    "x______.^ 
            |           \    
            j            Y