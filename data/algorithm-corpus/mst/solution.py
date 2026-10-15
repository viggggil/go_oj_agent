n,m=map(int,input().split()); edges=[]
for _ in range(m):
    u,v,w=map(int,input().split()); edges.append((w,u-1,v-1))
p=list(range(n)); size=[1]*n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
ans=cnt=0
for w,u,v in sorted(edges):
    a,b=find(u),find(v)
    if a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]; ans+=w; cnt+=1
print(ans if cnt==n-1 else -1)
