n,m,q=map(int,input().split()); inf=10**30; d=[[inf]*n for _ in range(n)]
for i in range(n): d[i][i]=0
for _ in range(m):
    u,v,w=map(int,input().split()); d[u-1][v-1]=min(d[u-1][v-1],w)
for k in range(n):
    for i in range(n):
        for j in range(n): d[i][j]=min(d[i][j],d[i][k]+d[k][j])
for _ in range(q):
    s,t=map(int,input().split()); print(-1 if d[s-1][t-1]==inf else d[s-1][t-1])
