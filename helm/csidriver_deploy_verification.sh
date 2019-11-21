#!/bin/sh
#set -e
n=0
namespacevar=unity
# verification of  nodes running
echo "########### NODES OF CLUSTER IN READY STATE #############"
nodescount=$(kubectl get nodes -o wide --namespace $namespacevar | awk ' /Ready/{ print $2; }'| wc -l)
         if [ $nodescount -gt $n ]
         then
                echo 'Nodes are  successfully deployed in the  cluster.'
         else
                echo 'Nodes are not deployed in the  cluster.'
                exit 1
         fi
         getnodesrunning=$(kubectl get nodes -o wide --namespace $namespacevar |grep -v NAME | awk ' /Ready/{ print $1;}')
         if [  "$getnodesrunning" != " " ]
         then
                echo   "List of nodes in Ready state of the cluster : "
                        for i in $getnodesrunning
                        do
                                echo $i
                        done
        else
               echo  "No nodes identified in the cluster :"
                exit 1
        fi


#  verify  pods of controller and nodes.
echo "########### PODS AND THEIR RUNNING STATUS #############"
max=300
totaltimespent=0
i=10
podscount=$(kubectl get pods -o wide --namespace $namespacevar | grep -v NAME | awk '{print $1;}'| wc -l)

        if [ $podscount -gt $n ]
        then
                up=0
                while  [ $up -lt $podscount ] && [ $totaltimespent -lt $max ]

                do
                    sleep $i
                    totaltimespent=$(( totaltimespent + i ))
                    up=$(kubectl get pods --namespace $namespacevar | grep 'Running' | wc -l)

                done
                if [ $up -lt $podscount ]
                then
                        echo  "All pods are not running in the cluster"
                        exit 1
                else
                        echo  "All the pods are running in cluster "
                fi
        else
                echo "Deployment of pods in the cluster is unsuccessful"
                  exit 1
        fi

podsrunning=$(kubectl get pods -o wide --namespace  $namespacevar | grep -v NAME | awk '/Running/{ print $1;}')
              if [  "$podsrunning" != " " ]
              then
                       echo   "Pods which are running in the cluster : "
                             for i in $podsrunning
                                  do
                                      echo $i
                                  done
              else
                    echo  "No nodes identified in the cluster :"
                     exit 1
              fi

  #verify that storage class has been created.

echo "########### STORAGECLASS AND ITS RUNNING STATUS #############"
storageclassnum=$(kubectl get storageclass --namespace  $namespacevar | grep -v NAME| wc -l)
        if [ $storageclassnum -gt $n ]
        then
              echo 'Storage class has been  successfully Created.'
        else
              echo 'Storage class has not been created in the  cluster.'
              exit 1
        fi
storageclass=$(kubectl get storageclass --namespace  $namespacevar | grep -v NAME | awk '{ print $1; }')
        if [  "$storageclass" != " " ]
        then
              echo  "Storageclass created  in  the cluster  : "
              for i in $storageclass
                  do
                      echo $i
                  done
        else
                      echo  "No Storage class  identified in the cluster :"
                       exit 1
        fi

echo "################### statefulset pod ########################"
 statefulsetservice=$(kubectl get statefulset  --namespace  $namespacevar|grep -v NAME |  awk '{ print $1; }')
  statefulsetstatus=$(kubectl get statefulset  --namespace  $namespacevar |grep -v NAME |  awk '{ print $2; }')
        if [  "$statefulsetservice" != " " ] && [ "$statefulsetstatus" = "1/1" ]
        then
              echo  " statefulset service is running, statefulset name and status ::  " $statefulsetserice  $statefulsetstatus

        else
                      echo  "Statefulset service  is not running :"
                       exit 1
        fi
echo "################### daemonset pods #######################"
          daemonsetnodename=$(kubectl get daemonset   --namespace  $namespacevar |grep -v NAME |  awk '{ print $1; }')
          desirednodes=$(kubectl get daemonset  --namespace  $namespacevar |grep -v NAME |  awk '{ print $2; }')
          availablenodes=$(kubectl get daemonset  --namespace  $namespacevar |grep -v NAME |  awk '{ print $6; }')
                if [  "$daemonsetnodename" != " " ] && [ "$availablenodes" != " " ] && [ "$desirednodes" != " " ]
                then
                    if [ $desirednodes = $availablenodes ]; then
                      echo "daemonset service  are running, daemon service name desirednodes availablenodes  ::   " $daemonsetnodename $desirednodes $availablenodes
                    else
                      echo " All the available nodes of daemon set service are not running"
                      exit 1
                    fi
                else
                              echo  "No daemonset pods are running  :"
                               exit 1
                fi
